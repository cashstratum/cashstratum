/*
 * Copyright 2026 Con Kolivas
 *
 * This program is free software; you can redistribute it and/or modify it
 * under the terms of the GNU General Public License as published by the Free
 * Software Foundation; either version 3 of the License, or (at your option)
 * any later version.  See COPYING for more details.
 *
 * Stratum V1 hostile input. Frames a misbehaving or malicious miner can send
 * (wrong types, missing members, bad hex, absurd numbers and names, nested
 * junk) are fed to the real method dispatcher and parsers in
 * src/stratifier.c. Each case names the frame it sends and what must come
 * back; none of them may crash the process or be accepted as valid.
 *
 * Replies are captured where the stratifier hands them to the connector: the
 * send queue is replaced by a recorder and drained through the real
 * ssend_process(), so the bytes checked are the bytes a miner would read.
 * Authorisation of a well formed worker needs the user tables and is not
 * driven here; every rejection that precedes it is.
 */
#include "config.h"
#include <assert.h>
#include <stdarg.h>
#include <stdio.h>

#include "../src/stratifier.c"

/* The daemon defines these in ckpool.c, which this test does not link. */
ckpool_t ckpool;

static int failures;

void logmsg(int loglevel, const char *fmt, ...)
{
	(void)loglevel; (void)fmt;
}

/* Entry points of other processes and of unrelated server paths. The frames
 * here never reach them, so reaching one is a failure. */
bool generator_checkaddr(const char *addr, bool *script, bool *segwit)
{
	(void)addr; (void)script; (void)segwit;
	abort();
}

bool generator_submitblock(const char *buf)
{
	(void)buf;
	abort();
}

void generator_preciousblock(const char *hash)
{
	(void)hash;
	abort();
}

bool generator_get_blockhash(int height, char *hash)
{
	(void)height; (void)hash;
	abort();
}

void generator_add_send(yyjson_mut_doc *doc)
{
	(void)doc;
	abort();
}

void connector_upstream_msg(char *msg)
{
	(void)msg;
	abort();
}

bool yyjson_mut_obj_get_string(char **store, yyjson_mut_val *val, const char *res)
{
	(void)store; (void)val; (void)res;
	abort();
}

bool yyjson_mut_obj_get_int64(int64_t *store, yyjson_mut_val *val, const char *res)
{
	(void)store; (void)val; (void)res;
	abort();
}

bool yyjson_mut_obj_get_int(int *store, yyjson_mut_val *val, const char *res)
{
	(void)store; (void)val; (void)res;
	abort();
}

bool yyjson_mut_obj_get_double(double *store, yyjson_mut_val *val, const char *res)
{
	(void)store; (void)val; (void)res;
	abort();
}

#ifdef HAVE_SV2
/* Reached only for virtual SV2 sessions; V1 frames never create one. */
void sv2_strat_set_instance_diff(int64_t instance_id, double diff)
{
	(void)instance_id; (void)diff;
	abort();
}

void sv2_strat_clear_instance(int64_t instance_id)
{
	(void)instance_id;
	abort();
}
#endif

static sdata_t sdata;
static workbase_t wb;

/* Everything the stratifier queues for sending is recorded here. */
#define MAX_SENDS 16
static smsg_t *sent[MAX_SENDS];
static int nsent;
static int queued_other;	/* work queued for authorise/share/txn threads */
static char reply[MAX_SENDS][2048];
static int nreply;

bool _ckmsgq_add(ckmsgq_t *ckmsgq, void *data, bool head, const char *file,
		 const char *func, const int line)
{
	(void)head; (void)file; (void)func; (void)line;
	if (ckmsgq == sdata.ssends) {
		assert(nsent < MAX_SENDS);
		sent[nsent++] = data;
		return true;
	}
	/* Queued for another thread: count it and release it. */
	queued_other++;
	discard_json_params(data);
	return true;
}

/* What ssend_process() hands to the connector. A NULL root serialises to
 * NULL, which the real connector reports and drops. */
void _connector_add_yymessage(yyjson_mut_doc *doc, const char *file,
			      const char *func, const int line)
{
	char *buf;

	(void)file; (void)func; (void)line;
	assert(doc);
	buf = yyjson_mut_write(doc, 0, NULL);
	assert(nreply < MAX_SENDS);
	snprintf(reply[nreply++], sizeof(reply[0]), "%s", buf ? buf : "<null>");
	free(buf);
	yyjson_mut_doc_free(doc);
}

static int drops;
static int last_drop_reason;

void _queue_proc(proc_instance_t *pi, const char *msg, const char *file,
		 const char *func, const int line)
{
	(void)pi; (void)file; (void)func; (void)line;
	if (!strncmp(msg, "dropclient=", 11)) {
		drops++;
		sscanf(strchr(msg, ',') + 1, "%d", &last_drop_reason);
	}
}


/* No server accepts remote, node or passthrough roles. */
static bool server_flags[4];

static void reset_world(void)
{
	int i;

	for (i = 0; i < nreply; i++)
		reply[i][0] = '\0';
	nsent = nreply = queued_other = drops = last_drop_reason = 0;
	memset(&sdata, 0, sizeof(sdata));
	memset(&wb, 0, sizeof(wb));
	mutex_init(&sdata.uastats_lock);
	cklock_init(&sdata.workbase_lock);
	cklock_init(&sdata.instance_lock);
	sdata.ssends = (ckmsgq_t *)&sdata;	/* identity only, never dereferenced */
	sdata.sauthq = (ckmsgq_t *)&wb;
	sdata.sshareq = (ckmsgq_t *)&wb;
	sdata.stxnq = (ckmsgq_t *)&wb;
	sdata.current_workbase = &wb;
	ckpool.sdata = &sdata;
	ckpool.version_mask = 0x1fffe000;
	ckpool.trusted = server_flags;
	ckpool.nodeserver = server_flags;
	ckpool.passthroughserver = server_flags;
	ckpool.node = ckpool.proxy = ckpool.btcsolo = ckpool.remote = false;
}

typedef struct {
	bool subscribed;
	bool authorised;
	bool useragent;
} cstate_t;

static void init_client_state(stratum_instance_t *client, const cstate_t *st)
{
	memset(client, 0, sizeof(*client));
	client->id = 7;
	strcpy(client->identity, "7");
	client->sdata = &sdata;
	client->subscribed = st->subscribed;
	client->authorised = st->authorised;
	if (st->useragent)
		client->useragent = ckzalloc(1);
	if (st->authorised)
		client->workername = strdup("user.w1");
}

static void free_client_state(stratum_instance_t *client)
{
	free(client->useragent);
	free(client->workername);
}

/* Run every queued send through the real sender and return the Nth reply. */
static void drain_sends(void)
{
	int i, n = nsent;

	nsent = 0;
	for (i = 0; i < n; i++)
		ssend_process(sent[i]);
}

static const char *run_instance_frame(stratum_instance_t *client, const char *frame)
{
	yyjson_doc *idoc = yyjson_read(frame, strlen(frame), 0);
	smsg_t msg;

	if (!idoc) {
		fprintf(stderr, "FAIL test frame is not valid JSON: %s\n", frame);
		failures++;
		return NULL;
	}
	msg.doc = yyjson_doc_mut_copy(idoc, &ckyyalc);
	msg.client_id = client->id;
	parse_instance_msg(&sdata, &msg, client);
	drain_sends();
	yyjson_mut_doc_free(msg.doc);
	yyjson_doc_free(idoc);
	return nreply ? reply[0] : NULL;
}

#define CHECK(cond, name, frame) do { \
	if (!(cond)) { \
		fprintf(stderr, "FAIL %s: %s\n  frame: %s\n", name, #cond, frame); \
		failures++; \
	} \
} while (0)

typedef struct {
	const char *name;
	const char *frame;
	cstate_t st;
	const char *expect;	/* substring of the reply; NULL = no reply */
	bool expect_drop;
	bool expect_queued;	/* handed to a worker queue */
} dispatch_case_t;

#define U { false, false, false }	/* fresh connection */
#define S { true, false, true }		/* subscribed */
#define A { true, true, true }		/* subscribed and authorised */

/* A configure frame whose params nest arrays 200 deep, built at start-up. */
static char nested_frame[512];

static void build_nested_frame(void)
{
	const char *head = "{\"id\":1,\"method\":\"mining.configure\",\"params\":";
	char *p = nested_frame;
	int i;

	p += sprintf(p, "%s", head);
	for (i = 0; i < 200; i++)
		*p++ = '[';
	for (i = 0; i < 200; i++)
		*p++ = ']';
	strcpy(p, "}");
}

static const dispatch_case_t dispatch_cases[] = {
	/* Envelope: what a frame must carry to be dispatched at all. */
	{ "no method",
	  "{\"id\":1,\"params\":[]}", U,
	  "-3:method not found", false, false },
	{ "method is a number",
	  "{\"id\":1,\"method\":5,\"params\":[]}", U,
	  "method is not string", false, false },
	{ "method is an array",
	  "{\"id\":1,\"method\":[\"mining.subscribe\"],\"params\":[]}", U,
	  "method is not string", false, false },
	{ "method is null",
	  "{\"id\":1,\"method\":null,\"params\":[]}", U,
	  "method is not string", false, false },
	{ "subscribe without params",
	  "{\"id\":1,\"method\":\"mining.subscribe\"}", U,
	  "params not found", false, false },
	{ "result frame without method is ignored",
	  "{\"id\":1,\"result\":true}", U, NULL, false, false },
	{ "null id on unknown method gets no reply",
	  "{\"id\":null,\"method\":\"x\",\"params\":[]}", S, NULL, false, false },
	{ "empty method string gets no reply",
	  "{\"id\":1,\"method\":\"\",\"params\":[]}", S, NULL, false, false },
	{ "id is an object, echoed in the error",
	  "{\"id\":{\"a\":[1,2]},\"params\":[]}", U,
	  "\"id\":{\"a\":[1,2]}", false, false },
	{ "id is a very long string, echoed in the error",
	  "{\"id\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\","
	  "\"params\":[]}", U, "method not found", false, false },
	{ "duplicate keys: first one wins, still dispatched",
	  "{\"id\":1,\"method\":\"mining.configure\",\"method\":5,\"params\":[]}", U,
	  "version-rolling.mask", false, false },

	/* mining.configure: params are never read, so no value in them may
	 * change the pool's reply. */
	{ "configure with empty params",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":[]}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with params a string",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":\"x\"}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with params an object",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":{\"a\":1}}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure asking for a mask outside the pool mask",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":[[\"version-rolling\"],"
	  "{\"version-rolling.mask\":\"ffffffff\",\"version-rolling.min-bit-count\":99}]}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with a negative mask",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":[[\"version-rolling\"],"
	  "{\"version-rolling.mask\":-1}]}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with minimum-difficulty 1e308",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":[[\"minimum-difficulty\"],"
	  "{\"minimum-difficulty\":1e308}]}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with minimum-difficulty -1",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":[[\"minimum-difficulty\"],"
	  "{\"minimum-difficulty\":-1}]}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with minimum-difficulty 0",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":[[\"minimum-difficulty\"],"
	  "{\"minimum-difficulty\":0}]}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with minimum-difficulty a string",
	  "{\"id\":1,\"method\":\"mining.configure\",\"params\":[[\"minimum-difficulty\"],"
	  "{\"minimum-difficulty\":\"1e999\"}]}", U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure with params nested 200 deep",
	  nested_frame, U,
	  "\"version-rolling.mask\":\"1fffe000\"", false, false },
	{ "configure echoes a string id",
	  "{\"id\":\"cfg\",\"method\":\"mining.configure\",\"params\":[]}", U,
	  "\"id\":\"cfg\"", false, false },
	{ "configure echoes an array id",
	  "{\"id\":[1,[2]],\"method\":\"mining.configure\",\"params\":[]}", U,
	  "\"id\":[1,[2]]", false, false },
	{ "configure prefix match: mining.configureXYZ",
	  "{\"id\":1,\"method\":\"mining.configureXYZ\",\"params\":[]}", U,
	  "version-rolling.mask", false, false },
	{ "configure method case: MINING.CONFIGURE",
	  "{\"id\":1,\"method\":\"MINING.CONFIGURE\",\"params\":[]}", U,
	  "version-rolling.mask", false, false },

	/* Frames with no id at all. The pool builds the reply from the id it
	 * was given; a frame without one must be survived, and gets no
	 * reply it could be matched to. */
	{ "configure without id",
	  "{\"method\":\"mining.configure\",\"params\":[]}", U,
	  "<null>", false, false },
	{ "subscribe without id",
	  "{\"method\":\"mining.subscribe\",\"params\":[]}", U,
	  "<null>", false, false },
	{ "subscribe without id, params not an array",
	  "{\"method\":\"mining.subscribe\",\"params\":{}}", U,
	  "<null>", false, false },

	/* mining.subscribe parameters. With no usable workbase the pool
	 * answers "Initialising"; none of the shapes below may change that. */
	{ "subscribe with params an object",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":{}}", U,
	  "params not an array", false, false },
	{ "subscribe with params a number",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":5}", U,
	  "params not an array", false, false },
	{ "subscribe with params null",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":null}", U,
	  "params not an array", false, false },
	{ "subscribe with params [123]",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[123]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with a session id that is a number",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"ua\",5]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with a session id that is null",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"ua\",null]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with a session id that is an object",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"ua\",{\"a\":1}]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with a session id of odd length",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"ua\",\"abc\"]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with a session id that is not hex",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"ua\",\"zzzzzzzz\"]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with a session id of 40 hex digits",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"ua\","
	  "\"ffffffffffffffffffffffffffffffffffffffff\"]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with an absurd user agent",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"NiceHash/\\u0000\\n\\u007f"
	  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\"]}", U,
	  "\"id\":1", false, false },
	{ "subscribe with an empty user agent",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"\"]}", U,
	  "\"id\":1", false, false },
	{ "second subscribe on a subscribed connection gets no reply",
	  "{\"id\":1,\"method\":\"mining.subscribe\",\"params\":[\"ua\"]}", S,
	  NULL, false, false },

	/* mining.authorise dispatch: queued for the authorise thread, whose
	 * parser is covered below. Before subscribing a miner is dropped only
	 * once it sends something that needs a subscription. */
	{ "authorise on a subscribed connection is queued",
	  "{\"id\":2,\"method\":\"mining.authorize\",\"params\":[\"u\",\"x\"]}", S,
	  NULL, false, true },
	{ "authorise prefix match: mining.authXYZ is queued as an authorise",
	  "{\"id\":2,\"method\":\"mining.authXYZ\",\"params\":[]}", S,
	  NULL, false, true },
	{ "second authorise on an authorised connection gets no reply",
	  "{\"id\":2,\"method\":\"mining.authorize\",\"params\":[\"u\",\"x\"]}", A,
	  NULL, false, false },

	/* Everything past subscribe needs a subscribed, authorised client. */
	{ "submit before subscribe is dropped",
	  "{\"id\":3,\"method\":\"mining.submit\",\"params\":[]}", U,
	  NULL, true, false },
	{ "unknown method before subscribe is dropped",
	  "{\"id\":3,\"method\":\"mining.bogus\",\"params\":[]}", U,
	  NULL, true, false },
	{ "unknown method after subscribe, before authorise, is ignored",
	  "{\"id\":3,\"method\":\"mining.bogus\",\"params\":[]}", S,
	  NULL, false, false },
	{ "unknown method when authorised gets no reply",
	  "{\"id\":3,\"method\":\"mining.bogus\",\"params\":[]}", A,
	  NULL, false, false },
	/* mining.term goes to drop_client(), which acts on the instance table
	 * and leaves nothing for this test to observe beyond surviving it. */
	{ "mining.term gets no reply",
	  "{\"id\":3,\"method\":\"mining.term\",\"params\":[]}", S,
	  NULL, false, false },
	{ "submit from an authorised client is queued, whatever its params",
	  "{\"id\":3,\"method\":\"mining.submit\",\"params\":\"x\"}", A,
	  NULL, false, true },
	{ "submit with no id from an authorised client is queued",
	  "{\"method\":\"mining.submit\",\"params\":[]}", A,
	  NULL, false, true },
	{ "get_transactions with junk params is queued",
	  "{\"id\":4,\"method\":\"mining.get_transactions\",\"params\":[null,{},[]]}", A,
	  NULL, false, true },
	{ "get_transactions before authorise is ignored",
	  "{\"id\":4,\"method\":\"mining.get_transactions\",\"params\":[\"zz\"]}", S,
	  NULL, false, false },
	{ "remote method on a server that is not trusted is refused",
	  "{\"id\":4,\"method\":\"mining.remote\",\"params\":[]}", S,
	  NULL, true, false },
	{ "passthrough method on a normal server is refused",
	  "{\"id\":4,\"method\":\"mining.passthrough\",\"params\":[]}", S,
	  NULL, true, false },
	{ "node method on a normal server is refused",
	  "{\"id\":4,\"method\":\"mining.node\",\"params\":[]}", S,
	  NULL, true, false },
};

static void test_dispatch(void)
{
	size_t i;

	for (i = 0; i < sizeof(dispatch_cases) / sizeof(dispatch_cases[0]); i++) {
		const dispatch_case_t *c = &dispatch_cases[i];
		stratum_instance_t client;
		const char *got;

		reset_world();
		init_client_state(&client, &c->st);
		got = run_instance_frame(&client, c->frame);
		if (c->expect) {
			CHECK(got != NULL, c->name, c->frame);
			CHECK(got && strstr(got, c->expect), c->name, c->frame);
			if (got && !strstr(got, c->expect))
				fprintf(stderr, "  got: %s\n  want substring: %s\n", got, c->expect);
		} else {
			CHECK(got == NULL, c->name, c->frame);
			if (got)
				fprintf(stderr, "  unexpected reply: %s\n", got);
		}
		CHECK((drops > 0 || client.drop_recorded) == c->expect_drop, c->name, c->frame);
		CHECK((queued_other > 0) == c->expect_queued, c->name, c->frame);
		free_client_state(&client);
	}
}

/* ---- parse_subscribe, called directly ---- */

typedef struct {
	const char *name;
	const char *params;
	const char *expect;	/* substring of the result; NULL = no result doc */
} sub_case_t;

static const sub_case_t sub_cases[] = {
	{ "params an object", "{}", "params not an array" },
	{ "params a string", "\"x\"", "params not an array" },
	{ "params null", "null", "params not an array" },
	{ "params empty", "[]", "Initialising" },
	{ "user agent a number", "[123]", "Initialising" },
	{ "user agent null", "[null]", "Initialising" },
	{ "session id a number", "[\"ua\",5]", "Initialising" },
	{ "session id an array", "[\"ua\",[1]]", "Initialising" },
	{ "session id empty", "[\"ua\",\"\"]", "Initialising" },
	{ "session id 9 hex digits", "[\"ua\",\"123456789\"]", "Initialising" },
	{ "session id odd", "[\"ua\",\"123\"]", "Initialising" },
	{ "extra members", "[\"ua\",\"00000000\",1,2,3,4,5,6,7,8,9]", "Initialising" },
};

static void test_parse_subscribe(void)
{
	size_t i;

	for (i = 0; i < sizeof(sub_cases) / sizeof(sub_cases[0]); i++) {
		const sub_case_t *c = &sub_cases[i];
		yyjson_doc *idoc = yyjson_read(c->params, strlen(c->params), 0);
		yyjson_mut_doc *doc, *res;
		stratum_instance_t client;
		cstate_t st = U;
		char *out;

		reset_world();
		sdata.current_workbase = NULL;	/* "Initialising": no workbase yet */
		init_client_state(&client, &st);
		doc = yyjson_doc_mut_copy(idoc, &ckyyalc);
		res = parse_subscribe(&client, client.id, yyjson_mut_doc_get_root(doc));
		CHECK(res != NULL, c->name, c->params);
		out = res ? yyjson_mut_write(res, 0, NULL) : NULL;
		CHECK(out && strstr(out, c->expect), c->name, c->params);
		CHECK(!client.subscribed, c->name, c->params);
		free(out);
		if (res)
			yyjson_mut_doc_free(res);
		yyjson_mut_doc_free(doc);
		yyjson_doc_free(idoc);
		free(client.useragent);
	}
}

/* ---- parse_authorise: every rejection before a user record exists ---- */

typedef struct {
	const char *name;
	const char *params;
	bool subscribed;
	const char *expect;
} auth_case_t;

static const auth_case_t auth_cases[] = {
	{ "params an object", "{}", true, "params not an array" },
	{ "params a string", "\"u\"", true, "params not an array" },
	{ "params null", "null", true, "params not an array" },
	{ "params empty", "[]", true, "params missing array entries" },
	{ "authorise before subscribe", "[\"u\",\"x\"]", false, "Failed subscription" },
	{ "worker name null", "[null,\"x\"]", true, "Invalid workername parameter" },
	{ "worker name a number", "[5]", true, "Invalid workername parameter" },
	{ "worker name an array", "[[\"u\"],\"x\"]", true, "Invalid workername parameter" },
	{ "worker name an object", "[{\"u\":1},\"x\"]", true, "Invalid workername parameter" },
	{ "worker name empty", "[\"\"]", true, "Empty workername parameter" },
	{ "worker name starts with a dot", "[\".w\",\"x\"]", true, "Empty username parameter" },
	{ "worker name starts with an underscore", "[\"_w\"]", true, "Empty username parameter" },
	{ "worker name holds a slash", "[\"a/b\"]", true, "Invalid character in username" },
	{ "worker name holds a path", "[\"a/../../etc/passwd\"]", true, "Invalid character in username" },
	{ "worker name holds a newline", "[\"u\\nforged\",\"x\"]", true, "Control character in username" },
	{ "worker name holds a carriage return", "[\"u\\rforged\"]", true, "Control character in username" },
	{ "worker name holds a tab", "[\"u\\tw\"]", true, "Control character in username" },
	{ "worker name holds DEL", "[\"u\\u007f\"]", true, "Control character in username" },
	{ "worker name holds an escape", "[\"u\\u001b[2J\"]", true, "Control character in username" },
	{ "worker name is only a newline", "[\"\\n\"]", true, "Control character in username" },
	{ "slash before control character", "[\"a/\\nb\"]", true, "Invalid character in username" },
	{ "NUL first makes an empty name", "[\"\\u0000abc\",\"x\"]", true, "Empty workername parameter" },
};

static void test_parse_authorise(void)
{
	size_t i;

	for (i = 0; i < sizeof(auth_cases) / sizeof(auth_cases[0]); i++) {
		const auth_case_t *c = &auth_cases[i];
		yyjson_doc *idoc = yyjson_read(c->params, strlen(c->params), 0);
		yyjson_mut_doc *doc, *err = NULL;
		stratum_instance_t client;
		cstate_t st = { c->subscribed, false, c->subscribed };
		char *out;
		bool ret;

		reset_world();
		init_client_state(&client, &st);
		doc = yyjson_doc_mut_copy(idoc, &ckyyalc);
		ret = parse_authorise(&client, yyjson_mut_doc_get_root(doc), &err);
		CHECK(!ret, c->name, c->params);
		CHECK(err != NULL, c->name, c->params);
		out = err ? yyjson_mut_write(err, 0, NULL) : NULL;
		CHECK(out && strstr(out, c->expect), c->name, c->params);
		if (out && !strstr(out, c->expect))
			fprintf(stderr, "  got: %s\n  want: %s\n", out, c->expect);
		CHECK(!client.authorised && !client.workername, c->name, c->params);
		free(out);
		if (err)
			yyjson_mut_doc_free(err);
		yyjson_mut_doc_free(doc);
		yyjson_doc_free(idoc);
		free(client.useragent);
	}
}

/* ---- parse_submit: every rejection that precedes the job lookup ---- */

typedef struct {
	const char *name;
	const char *params;
	enum share_err expect;
} submit_case_t;

#define W "\"user.w1\""
static const submit_case_t submit_cases[] = {
	{ "params a string", "\"x\"", SE_NOT_ARRAY },
	{ "params an object", "{\"a\":1}", SE_NOT_ARRAY },
	{ "params null", "null", SE_NOT_ARRAY },
	{ "params empty", "[]", SE_INVALID_SIZE },
	{ "four members", "[" W ",\"1\",\"00000000\",\"5f5e1000\"]", SE_INVALID_SIZE },
	{ "worker name null", "[null,\"1\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_NO_USERNAME },
	{ "worker name a number", "[5,\"1\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_NO_USERNAME },
	{ "worker name empty", "[\"\",\"1\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_NO_USERNAME },
	{ "job id null", "[" W ",null,\"00000000\",\"5f5e1000\",\"00000000\"]", SE_NO_JOBID },
	{ "job id a number", "[" W ",1,\"00000000\",\"5f5e1000\",\"00000000\"]", SE_NO_JOBID },
	{ "job id empty", "[" W ",\"\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_NO_JOBID },
	{ "nonce2 not hex", "[" W ",\"1\",\"zz\",\"5f5e1000\",\"00000000\"]", SE_NO_NONCE2 },
	{ "nonce2 odd length", "[" W ",\"1\",\"abc\",\"5f5e1000\",\"00000000\"]", SE_NO_NONCE2 },
	{ "nonce2 empty", "[" W ",\"1\",\"\",\"5f5e1000\",\"00000000\"]", SE_NO_NONCE2 },
	{ "nonce2 a number", "[" W ",\"1\",12,\"5f5e1000\",\"00000000\"]", SE_NO_NONCE2 },
	{ "nonce2 an array", "[" W ",\"1\",[\"00\"],\"5f5e1000\",\"00000000\"]", SE_NO_NONCE2 },
	{ "nonce2 with a sign", "[" W ",\"1\",\"-1\",\"5f5e1000\",\"00000000\"]", SE_NO_NONCE2 },
	{ "nonce2 with 0x prefix", "[" W ",\"1\",\"0x00\",\"5f5e1000\",\"00000000\"]", SE_NO_NONCE2 },
	{ "ntime not hex", "[" W ",\"1\",\"00000000\",\"xyz\",\"00000000\"]", SE_NO_NTIME },
	{ "ntime odd length", "[" W ",\"1\",\"00000000\",\"5f5e100\",\"00000000\"]", SE_NO_NTIME },
	{ "ntime null", "[" W ",\"1\",\"00000000\",null,\"00000000\"]", SE_NO_NTIME },
	{ "ntime negative", "[" W ",\"1\",\"00000000\",\"-5f5e10\",\"00000000\"]", SE_NO_NTIME },
	{ "nonce 7 digits", "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"1234567\"]", SE_NO_NONCE },
	{ "nonce not hex", "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"zzzzzzzz\"]", SE_NO_NONCE },
	{ "nonce 9 digits (odd)", "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"123456789\"]", SE_NO_NONCE },
	{ "nonce a number", "[" W ",\"1\",\"00000000\",\"5f5e1000\",4294967295]", SE_NO_NONCE },
	{ "version mask outside pool mask",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"ffffffff\"]", SE_INVALID_VERSION_MASK },
	{ "version mask a single bit outside the pool mask",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"00000001\"]", SE_INVALID_VERSION_MASK },
	{ "version mask 80000000",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"80000000\"]", SE_INVALID_VERSION_MASK },
	{ "version mask 16 digits, high half ignored by scan",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"00000000ffffffff\"]", SE_INVALID_VERSION_MASK },
	{ "worker name differs",
	  "[\"other.w\",\"1\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_WORKER_MISMATCH },
	{ "worker name differs only in case",
	  "[\"USER.W1\",\"1\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_WORKER_MISMATCH },
	{ "worker name with a trailing character",
	  "[\"user.w1x\",\"1\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_WORKER_MISMATCH },
	/* A version mask that is not valid hex is ignored, not rejected: the
	 * share is judged on everything else. With no workbase that is the
	 * "no workbase" answer, which is still a rejection. */
	{ "version mask not hex is ignored",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"zz\"]", SE_NO_WORKBASE },
	{ "version mask odd length is ignored",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"123\"]", SE_NO_WORKBASE },
	{ "version mask a number is ignored",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",1]", SE_NO_WORKBASE },
	{ "version mask null is ignored",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",null]", SE_NO_WORKBASE },
	{ "version mask inside the pool mask",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"1fffe000\"]", SE_NO_WORKBASE },
	{ "job id not hex, no workbase to look it up in",
	  "[" W ",\"zzzz\",\"00000000\",\"5f5e1000\",\"00000000\"]", SE_NO_WORKBASE },
	{ "job id 40 hex digits, no workbase",
	  "[" W ",\"ffffffffffffffffffffffffffffffffffffffff\",\"00000000\",\"5f5e1000\",\"00000000\"]",
	  SE_NO_WORKBASE },
	{ "ntime 20 hex digits, no workbase",
	  "[" W ",\"1\",\"00000000\",\"ffffffffffffffffffff\",\"00000000\"]", SE_NO_WORKBASE },
	{ "extra members are ignored, no workbase",
	  "[" W ",\"1\",\"00000000\",\"5f5e1000\",\"00000000\",\"00000000\",1,2,3,4,5,6,7,8,9,10]",
	  SE_NO_WORKBASE },
};

static void test_parse_submit(void)
{
	size_t i;

	for (i = 0; i < sizeof(submit_cases) / sizeof(submit_cases[0]); i++) {
		const submit_case_t *c = &submit_cases[i];
		yyjson_doc *idoc = yyjson_read(c->params, strlen(c->params), 0);
		enum share_err err = SE_NONE;
		stratum_instance_t client;
		worker_instance_t worker;
		user_instance_t user;
		cstate_t st = A;
		yyjson_mut_doc *doc;
		bool ret;

		reset_world();
		sdata.current_workbase = NULL;	/* checks that precede the job lookup */
		init_client_state(&client, &st);
		memset(&worker, 0, sizeof(worker));
		memset(&user, 0, sizeof(user));
		client.worker_instance = &worker;
		client.user_instance = &user;
		doc = yyjson_doc_mut_copy(idoc, &ckyyalc);
		ret = parse_submit(&client, yyjson_mut_doc_get_root(doc), &err);
		CHECK(!ret, c->name, c->params);
		CHECK(err == c->expect, c->name, c->params);
		if (err != c->expect)
			fprintf(stderr, "  got share error %d, want %d\n", (int)err, (int)c->expect);
		yyjson_mut_doc_free(doc);
		yyjson_doc_free(idoc);
		free_client_state(&client);
	}
}

/* mining.suggest_difficulty values reach suggest_diff() only from an
 * authorised connection, and none of them may move the connection outside
 * the pool's bounds or crash the parser. */
static void test_suggest_difficulty(void)
{
	static const char *const frames[] = {
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[1e308]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[-5]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[0]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[\"x\"]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[null]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":{}}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[[1]]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[9223372036854775807]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty\",\"params\":[1e18,2]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty(99999999999999999999\",\"params\":[]}",
		"{\"id\":1,\"method\":\"mining.suggest_difficulty(-1\",\"params\":[]}",
	};
	size_t i;

	for (i = 0; i < sizeof(frames) / sizeof(frames[0]); i++) {
		stratum_instance_t client;
		cstate_t st = A;

		reset_world();
		ckpool.mindiff = 1;
		ckpool.startdiff = 8;
		ckpool.maxdiff = 1000000;
		init_client_state(&client, &st);
		client.diff = client.old_diff = 8;
		run_instance_frame(&client, frames[i]);
		CHECK(client.suggest_diff >= 0 && client.suggest_diff <= ckpool.maxdiff,
		      "suggest_difficulty stays within pool bounds", frames[i]);
		CHECK(drops == 0, "suggest_difficulty does not drop the connection", frames[i]);
		free_client_state(&client);
	}
}

int main(void)
{
	build_nested_frame();
	test_dispatch();
	test_parse_subscribe();
	test_parse_authorise();
	test_parse_submit();
	test_suggest_difficulty();
	if (failures) {
		fprintf(stderr, "v1_hostile: %d check(s) failed\n", failures);
		return 1;
	}
	puts("v1_hostile: all OK");
	return 0;
}
