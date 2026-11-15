/*
 * Standalone test for the coinbase scriptSig builder
 * (src/coinbase_scriptsig.c).
 *
 * The scriptSig is consensus-relevant: its length byte must match its real
 * length or the coinbase does not parse, it must be 2..100 bytes, and BIP34
 * requires the height first. Explorers and miner screens render it as text,
 * so the operator text must follow the height with no byte in between.
 * These vectors pin the exact bytes for real and boundary inputs.
 *
 * Distributed under the GPL v3. See COPYING for more details.
 */

#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>

#include "coinbase_final.h"
#include "coinbase_scriptsig.h"

/* From libckpool; libckpool.h is not needed for anything else here. */
int ser_number(unsigned char *s, int32_t val);

/* MAX_GBT_FLAGS_LEN in bitcoin.h */
#define MAX_FLAGS 32

static int failures;

#define CHECK(cond, ...) do {					\
	if (!(cond)) {						\
		printf("FAIL %s:%d: ", __FILE__, __LINE__);	\
		printf(__VA_ARGS__);				\
		printf("\n");					\
		failures++;					\
	}							\
} while (0)

static void tohex(char *out, const unsigned char *p, int len)
{
	static const char hex[] = "0123456789abcdef";
	int i;

	for (i = 0; i < len; i++) {
		out[i * 2] = hex[p[i] >> 4];
		out[i * 2 + 1] = hex[p[i] & 0xf];
	}
	out[len * 2] = '\0';
}

static void fromhex(unsigned char *out, const char *hex)
{
	size_t i, n = strlen(hex) / 2;

	for (i = 0; i < n; i++) {
		unsigned int b;

		sscanf(hex + i * 2, "%2x", &b);
		out[i] = (unsigned char)b;
	}
}

static int mainnet_height(unsigned char *buf, uint32_t height)
{
	unsigned char tmp[8];
	int len = ser_number(tmp, (int32_t)height);

	memcpy(buf, tmp, len);
	return len;
}

/* Assemble a whole coinbase the way the stratifier does -- 41-byte prefix,
 * length byte, scriptSig prefix, a zeroed extranonce, then nSequence
 * 0xffffffff, one output and nLockTime 0 -- and confirm the declared length
 * lands nSequence exactly where coinbase_finality() looks for it. */
static void check_assembled(const char *name, const unsigned char *prefix,
			    int prefixlen, int enonce_len, int scriptlen)
{
	unsigned char cb[512];
	uint32_t nseq = 0, nlock = 1;
	int ofs = 0;

	memset(cb, 0, sizeof(cb));
	memcpy(cb, "\x01\x00\x00\x00\x01", 5);
	memset(cb + 5, 0, 32);
	memset(cb + 37, 0xff, 4);
	ofs = 41;
	cb[ofs++] = (unsigned char)scriptlen;
	memcpy(cb + ofs, prefix, prefixlen);
	ofs += prefixlen;
	/* Declared length equals the scriptSig bytes in coinb1 plus the
	 * extranonce. */
	CHECK(ofs - 42 + enonce_len == scriptlen,
	      "%s: coinb1 scriptSig %d + extranonce %d != declared %d",
	      name, ofs - 42, enonce_len, scriptlen);
	ofs += enonce_len;
	memset(cb + ofs, 0xff, 4);
	ofs += 4;
	cb[ofs++] = 0x01;
	ofs += 8;
	cb[ofs++] = 0x01;
	cb[ofs++] = 0x51;
	ofs += 4;

	CHECK(coinbase_finality(cb, ofs, 1000, &nseq, &nlock) == CBF_FINAL,
	      "%s: assembled coinbase is not final", name);
	CHECK(nseq == 0xffffffffU && nlock == 0,
	      "%s: nSequence %08x nLockTime %u read at the declared offset",
	      name, nseq, nlock);
}

/* Build and compare against an expected prefix written out by hand. */
static void expect(const char *name, const unsigned char *height, int hlen,
		   const char *text, size_t textlen,
		   const unsigned char *flags, size_t flagslen,
		   const coinbase_stamp_t *stamp, int enonce_len,
		   const char *want_hex, int want_scriptlen)
{
	unsigned char out[COINBASE_SCRIPTSIG_PREFIX_CAP];
	char got_hex[COINBASE_SCRIPTSIG_PREFIX_CAP * 2 + 1];
	int len, scriptlen = -1, before = failures;

	memset(out, 0xee, sizeof(out));
	len = coinbase_scriptsig_prefix(out, sizeof(out), height, hlen, text,
					textlen, flags, flagslen, stamp,
					enonce_len, &scriptlen);
	CHECK(len > 0, "%s: builder failed (%d)", name, len);
	if (len <= 0)
		return;
	tohex(got_hex, out, len);
	CHECK(!strcmp(got_hex, want_hex), "%s:\n  got  %s\n  want %s",
	      name, got_hex, want_hex);
	CHECK(scriptlen == want_scriptlen, "%s: declared length %d, want %d",
	      name, scriptlen, want_scriptlen);
	CHECK(scriptlen == len + enonce_len,
	      "%s: declared length %d != written %d + extranonce %d",
	      name, scriptlen, len, enonce_len);
	CHECK(len == (int)sizeof(out) || out[len] == 0xee,
	      "%s: wrote past the reported length", name);
	CHECK(scriptlen >= COINBASE_SCRIPTSIG_MIN &&
	      scriptlen <= COINBASE_SCRIPTSIG_MAX,
	      "%s: declared length %d outside the consensus range",
	      name, scriptlen);
	check_assembled(name, out, len, enonce_len, scriptlen);
	if (failures == before)
		printf("ok   %-34s %2d bytes  %s\n", name, scriptlen, got_hex);
}

#define SIG33 "/BlockSniper.ai/ AI Shared Mining"
#define SIG33_HEX "2f426c6f636b536e697065722e61692f20414920536861726564204d696e696e67"
#define SIG38 "/BlockSniper.ai/ AI Shared Mining/0123"
#define SIG38_HEX SIG33_HEX "2f30313233"

int main(void)
{
	/* tv_sec 1794000000 = 0x6aee4480; tv_nsec 0x800000 needs a fourth
	 * byte because ser_number keeps the top bit of the last byte clear. */
	const coinbase_stamp_t stamp = { 1794000000, 0x800000 };
	unsigned char h[8], flags[MAX_FLAGS], out[COINBASE_SCRIPTSIG_PREFIX_CAP];
	char want[512];
	int hlen, len, scriptlen, i;

	/* Height 977526 = 0x0eea76 -> 03 76ea0e */
	hlen = mainnet_height(h, 977526);
	CHECK(hlen == 4 && !memcmp(h, "\x03\x76\xea\x0e", 4), "height 977526");

	/* 1: mainnet-like. The text starts at scriptSig byte 4, straight after
	 * the height, with no length byte; then empty flags, both time pushes
	 * and the extranonce length. 4 + 33 + 1 + 5 + 5 + 1 + 12 = 61. */
	expect("mainnet 33-byte btcsig", h, hlen, SIG33, strlen(SIG33), NULL, 0,
	       &stamp, 12,
	       "0376ea0e" SIG33_HEX "00" "048044ee6a" "0400008000" "0c", 61);
	{
		len = coinbase_scriptsig_prefix(out, sizeof(out), h, hlen, SIG33,
						strlen(SIG33), NULL, 0, &stamp,
						12, &scriptlen);
		CHECK(len == 49 && !memcmp(out + 4, SIG33, 33),
		      "text is not at scriptSig byte 4");
		CHECK(out[3] == 0x0e, "byte before the text is not the height");
	}

	/* 2: the 38-byte maximum btcsig */
	expect("38-byte btcsig", h, hlen, SIG38, strlen(SIG38), NULL, 0,
	       &stamp, 12,
	       "0376ea0e" SIG38_HEX "00" "048044ee6a" "0400008000" "0c", 66);

	/* 3: no text, NULL or empty */
	expect("NULL btcsig", h, hlen, NULL, 0, NULL, 0, &stamp, 12,
	       "0376ea0e" "00" "048044ee6a" "0400008000" "0c", 28);
	expect("empty btcsig", h, hlen, "", 0, NULL, 0, &stamp, 12,
	       "0376ea0e" "00" "048044ee6a" "0400008000" "0c", 28);
	expect("NULL btcsig with stale length", h, hlen, NULL, 5, NULL, 0,
	       &stamp, 12,
	       "0376ea0e" "00" "048044ee6a" "0400008000" "0c", 28);

	/* 4: non-empty flags, including the 32-byte maximum */
	expect("3-byte flags", h, hlen, SIG33, strlen(SIG33),
	       (const unsigned char *)"\x01\x02\x03", 3, &stamp, 12,
	       "0376ea0e" SIG33_HEX "03010203" "048044ee6a" "0400008000" "0c", 64);
	for (i = 0; i < MAX_FLAGS; i++)
		flags[i] = (unsigned char)(0xa0 + i);
	snprintf(want, sizeof(want), "0376ea0e%s20", SIG38_HEX);
	for (i = 0; i < MAX_FLAGS; i++)
		snprintf(want + strlen(want), 3, "%02x", flags[i]);
	strcat(want, "048044ee6a" "0400008000" "0c");
	/* 4 + 38 + 1 + 32 + 5 + 5 + 1 + 12 = 98, still under the cap */
	expect("38-byte btcsig, 32-byte flags", h, hlen, SIG38, strlen(SIG38),
	       flags, MAX_FLAGS, &stamp, 12, want, 98);

	/* Time values whose top bit needs a padding byte, and short ones,
	 * serialise exactly as ser_number does. */
	{
		const coinbase_stamp_t s2 = { 128, 0x8000 };
		const coinbase_stamp_t s3 = { 0, 127 };

		expect("time 128 / 0x8000", h, hlen, NULL, 0, NULL, 0, &s2, 12,
		       "0376ea0e" "00" "028000" "03008000" "0c", 25);
		expect("time 0 / 127", h, hlen, NULL, 0, NULL, 0, &s3, 12,
		       "0376ea0e" "00" "0100" "017f" "0c", 22);
	}

	/* Without the time pushes the extranonce length follows the flags. */
	expect("no time pushes", h, hlen, SIG33, strlen(SIG33), NULL, 0, NULL,
	       12, "0376ea0e" SIG33_HEX "00" "0c", 51);

	/* 5: regtest heights through the BIP34 minimal encoding */
	{
		static const struct { uint32_t height; const char *hex; } v[] = {
			{ 0, "00" }, { 1, "51" }, { 16, "60" }, { 17, "0111" },
			{ 127, "017f" }, { 128, "028000" }, { 255, "02ff00" },
			{ 256, "020001" }, { 32767, "02ff7f" },
			{ 32768, "03008000" }, { 977526, "0376ea0e" },
		};
		unsigned char hb[8], want_hb[8];
		char name[64];

		for (i = 0; i < (int)(sizeof(v) / sizeof(v[0])); i++) {
			memset(hb, 0xee, sizeof(hb));
			hlen = ser_bip34_height(hb, v[i].height);
			fromhex(want_hb, v[i].hex);
			CHECK(hlen == (int)strlen(v[i].hex) / 2 &&
			      !memcmp(hb, want_hb, hlen),
			      "bip34 height %u", v[i].height);
			snprintf(name, sizeof(name), "regtest height %u", v[i].height);
			snprintf(want, sizeof(want), "%s2f726567746573742d6532652f00"
				 "048044ee6a0400008000" "0c", v[i].hex);
			expect(name, hb, hlen, "/regtest-e2e/", 13, NULL, 0,
			       &stamp, 12, want, hlen + 13 + 1 + 5 + 5 + 1 + 12);
		}
		/* mainnet and BIP34 encodings agree for real heights */
		hlen = mainnet_height(h, 977526);
		CHECK(ser_bip34_height(hb, 977526) == hlen && !memcmp(hb, h, hlen),
		      "BIP34 and ser_number disagree at 977526");
	}

	/* 7: the worst case the daemon can build -- 38-byte text, 32-byte
	 * flags, both time pushes, 8 + 8 byte extranonce -- fits the buffer
	 * and is reported as over the consensus cap rather than truncated. */
	{
		int worst;

		memset(out, 0xee, sizeof(out));
		len = coinbase_scriptsig_prefix(out, sizeof(out), h, hlen, SIG38,
						38, flags, MAX_FLAGS, &stamp, 16,
						&worst);
		CHECK(len == 4 + 38 + 1 + 32 + 5 + 5 + 1, "worst case length %d", len);
		CHECK(worst == len + 16 && worst > COINBASE_SCRIPTSIG_MAX,
		      "worst case declared %d not reported over the cap", worst);
		CHECK(!memcmp(out + 4, SIG38, 38), "worst case text truncated");
		check_assembled("worst case", out, len, 16, worst);
		printf("ok   %-34s %2d bytes  (over the %d-byte cap, reported)\n",
		       "worst case", worst, COINBASE_SCRIPTSIG_MAX);

		/* Six-byte height as well still fits the documented cap. */
		hlen = ser_bip34_height(h, 0xffffffffU);
		CHECK(hlen == 6, "largest BIP34 height is %d bytes", hlen);
		len = coinbase_scriptsig_prefix(out, sizeof(out), h, hlen, SIG38,
						38, flags, MAX_FLAGS, &stamp, 16,
						&worst);
		CHECK(len == 88 && len <= COINBASE_SCRIPTSIG_PREFIX_CAP,
		      "largest prefix %d", len);
		hlen = mainnet_height(h, 977526);
	}

	/* Out-of-range inputs and a short buffer fail instead of writing. */
	{
		int sl = -7;

		memset(out, 0xee, sizeof(out));
		CHECK(coinbase_scriptsig_prefix(out, 40, h, hlen, SIG38, 38, NULL,
						0, &stamp, 12, &sl) == -1 && sl == -7,
		      "short buffer accepted");
		for (i = 40; i < (int)sizeof(out); i++)
			CHECK(out[i] == 0xee, "short buffer: wrote at %d", i);
		CHECK(coinbase_scriptsig_prefix(out, sizeof(out), h, hlen, NULL, 0,
						NULL, 0, &stamp, 76, &sl) == -1,
		      "extranonce of 76 accepted");
		CHECK(coinbase_scriptsig_prefix(out, sizeof(out), h, hlen, NULL, 0,
						NULL, 256, &stamp, 12, &sl) == -1,
		      "flags of 256 accepted");
		CHECK(coinbase_scriptsig_prefix(out, sizeof(out), h, 0, NULL, 0,
						NULL, 0, &stamp, 12, &sl) == -1,
		      "missing height accepted");
		CHECK(coinbase_scriptsig_prefix(out, sizeof(out), h, hlen, NULL, 0,
						NULL, 0, &stamp, 12, NULL) == -1,
		      "NULL scriptlen accepted");
	}

	if (failures) {
		printf("%d failure(s)\n", failures);
		return 1;
	}
	printf("all coinbase scriptSig vectors passed\n");
	return 0;
}
