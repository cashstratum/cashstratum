/*
 * Standalone test for node RPC compatibility in src/bitcoin.c.
 *
 * CashStratum talks to the node through a handful of fixed JSON-RPC request
 * strings and one getblocktemplate parser. BCHN is forgiving about both, so
 * the regtest gate cannot catch a request or template shape that only another
 * BCH node rejects. These cases pin the behaviour those nodes rely on:
 *
 *   - every request carries an "id": BCHD treats a request without one as a
 *     JSON-RPC notification and never answers it;
 *   - getblocktemplate does not ask for the coinbasetxn capability: BCHD then
 *     returns a coinbasetxn instead of coinbasevalue, or an error when it has
 *     no --miningaddr;
 *   - coinbaseaux is optional (Flowee the Hub omits it);
 *   - a template without a usable coinbasevalue is refused, never read as 0.
 *
 * bitcoin.c is included directly so the node RPC layer from ckpool.c can be
 * replaced with canned replies; with subdir-objects, listing src/bitcoin.c as a
 * test source would collide with the daemon's own src/bitcoin.o. yyjson and its
 * ckalloc allocator are linked into the daemon rather than libckpool.a, so
 * nodecompat_yyjson.c and nodecompat_alloc.c build them as separate units.
 */

#include "../src/bitcoin.c"

#include <stdarg.h>
#include <stdio.h>

static int failures;
static const char *reply;
static char last_req[512];

#define CHECK(cond, ...) do { \
	if (!(cond)) { \
		failures++; \
		fprintf(stderr, "FAIL %s:%d: ", __FILE__, __LINE__); \
		fprintf(stderr, __VA_ARGS__); \
		fputc('\n', stderr); \
	} \
} while (0)

static void record(const char *rpc_req)
{
	snprintf(last_req, sizeof(last_req), "%s", rpc_req);
}

yyjson_doc *yyjson_rpc_call(connsock_t *cs, const char *rpc_req)
{
	(void)cs;
	record(rpc_req);
	return reply ? yyjson_read(reply, strlen(reply), 0) : NULL;
}

yyjson_doc *yyjson_rpc_response(connsock_t *cs, const char *rpc_req)
{
	return yyjson_rpc_call(cs, rpc_req);
}

void yyjson_rpc_msg(connsock_t *cs, const char *rpc_req)
{
	(void)cs;
	record(rpc_req);
}

void logmsg(int loglevel, const char *fmt, ...)
{
	(void)loglevel;
	(void)fmt;
}

/* Each check names the method it expects, so a call that returned early and
 * sent nothing cannot pass on the previous call's request. */
static void check_request_has_id(const char *method)
{
	char want[64];

	snprintf(want, sizeof(want), "\"method\": \"%s\"", method);
	CHECK(strstr(last_req, want) != NULL, "%s was not sent; last request: %s", method,
	      last_req[0] ? last_req : "(none)");
	CHECK(strstr(last_req, "\"id\": 0") != NULL, "%s request has no id: %s", method, last_req);
	last_req[0] = '\0';
}

#define HASH64 "000000000000000000a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f607"
#define TARGET64 "0000000000000000017c4f000000000000000000000000000000000000000000"
#define GBT_HEAD "{\"result\":{\"previousblockhash\":\"" HASH64 "\",\"target\":\"" TARGET64 "\"," \
	"\"version\":536870912,\"curtime\":1791273600,\"bits\":\"18017c4f\",\"height\":971745"
#define GBT_TAIL "},\"error\":null,\"id\":0}"

static void test_template(const char *name, const char *json, bool want_ok,
			  uint64_t want_value, const char *want_flags)
{
	connsock_t cs;
	gbtbase_t gbt;
	bool ok;

	memset(&cs, 0, sizeof(cs));
	memset(&gbt, 0, sizeof(gbt));
	reply = json;
	ok = gen_gbtbase(&cs, &gbt);
	check_request_has_id("getblocktemplate");
	CHECK(!strstr(last_req, "coinbasetxn"), "getblocktemplate still requests coinbasetxn");
	CHECK(ok == want_ok, "%s: gen_gbtbase returned %d, want %d", name, ok, want_ok);
	if (ok && want_ok) {
		CHECK(gbt.coinbasevalue == want_value, "%s: coinbasevalue %llu, want %llu", name,
		      (unsigned long long)gbt.coinbasevalue, (unsigned long long)want_value);
		CHECK(gbt.height == 971745, "%s: height %d", name, gbt.height);
		CHECK(gbt.flags && !strcmp(gbt.flags, want_flags), "%s: flags '%s', want '%s'", name,
		      gbt.flags ? gbt.flags : "(null)", want_flags);
	}
	if (ok)
		clear_gbtbase(&gbt);
}

static void test_templates(void)
{
	/* BCHN: coinbaseaux present, coinbasevalue present. */
	test_template("bchn", GBT_HEAD ",\"coinbasevalue\":312500000,\"coinbaseaux\":{\"flags\":\"0b2f4243484e2f\"}" GBT_TAIL,
		      true, 312500000, "0b2f4243484e2f");
	/* Flowee the Hub: no coinbaseaux at all. */
	test_template("no coinbaseaux", GBT_HEAD ",\"coinbasevalue\":312500000" GBT_TAIL,
		      true, 312500000, "");
	/* coinbaseaux without flags behaves the same. */
	test_template("empty coinbaseaux", GBT_HEAD ",\"coinbasevalue\":312500000,\"coinbaseaux\":{}" GBT_TAIL,
		      true, 312500000, "");
	/* BCHD with --miningaddr answers a coinbasetxn request with no
	 * coinbasevalue: refusing it is what stops a zero-value coinbase. */
	test_template("coinbasetxn only", GBT_HEAD ",\"coinbasetxn\":{\"data\":\"01\"}" GBT_TAIL,
		      false, 0, "");
	test_template("negative coinbasevalue", GBT_HEAD ",\"coinbasevalue\":-1" GBT_TAIL,
		      false, 0, "");
	test_template("string coinbasevalue", GBT_HEAD ",\"coinbasevalue\":\"312500000\"" GBT_TAIL,
		      false, 0, "");
	/* A genuinely required field is still required. */
	test_template("missing bits",
		      "{\"result\":{\"previousblockhash\":\"" HASH64 "\",\"target\":\"" TARGET64 "\","
		      "\"version\":536870912,\"curtime\":1791273600,\"height\":971745,"
		      "\"coinbasevalue\":312500000}" GBT_TAIL,
		      false, 0, "");
	/* No reply at all (BCHD's answer to a request with no id). */
	test_template("no reply", NULL, false, 0, "");
}

static void test_request_ids(void)
{
	connsock_t cs;
	char hash[68];
	yyjson_doc *doc;
	char *txn;

	memset(&cs, 0, sizeof(cs));
	cs.alive = true;

	reply = "{\"result\":971745,\"error\":null,\"id\":0}";
	CHECK(get_blockcount(&cs) == 971745, "get_blockcount");
	check_request_has_id("getblockcount");

	reply = "{\"result\":\"" HASH64 "\",\"error\":null,\"id\":0}";
	CHECK(get_blockhash(&cs, 971745, hash), "get_blockhash");
	check_request_has_id("getblockhash");

	CHECK(get_bestblockhash(&cs, hash), "get_bestblockhash");
	check_request_has_id("getbestblockhash");

	reply = "{\"result\":null,\"error\":null,\"id\":0}";
	CHECK(submit_block(&cs, "00"), "submit_block");
	check_request_has_id("submitblock");

	precious_block(&cs, HASH64);
	check_request_has_id("preciousblock");

	submit_txn(&cs, "00");
	check_request_has_id("sendrawtransaction");

	reply = "{\"result\":\"00\",\"error\":null,\"id\":0}";
	txn = get_txn(&cs, HASH64);
	check_request_has_id("getrawtransaction");
	free(txn);

	reply = "{\"result\":{\"txid\":\"" HASH64 "\"},\"error\":null,\"id\":0}";
	doc = validate_txn(&cs, "00");
	check_request_has_id("decoderawtransaction");
	if (doc)
		yyjson_doc_free(doc);
}

int main(void)
{
	test_templates();
	test_request_ids();
	if (failures) {
		fprintf(stderr, "nodecompat: %d failure(s)\n", failures);
		return 1;
	}
	puts("nodecompat: request ids, coinbasetxn-free GBT, optional coinbaseaux and coinbasevalue guard passed");
	return 0;
}
