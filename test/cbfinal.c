/*
 * Standalone test for the coinbase finality predicate
 * (src/coinbase_final.c).
 *
 * The predicate is what stands between a careless nSequence/nLockTime edit
 * and a solved block bitcoind rejects as bad-txns-nonfinal, and production
 * cannot exercise it: the pool's own coinbase is nLockTime 0, which is final
 * unconditionally, so every regtest and mainnet run takes the same single
 * branch. These vectors are the only place the other branches run at all.
 *
 * The four nSequence x nLockTime rows are the decision table derived
 * from Bitcoin's IsFinalTx.
 */

#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>

#include "coinbase_final.h"

static int failures;

/* Build a minimal but structurally real legacy coinbase:
 *   nVersion(4) | vin count(1) | null prevout(36) | scriptSig len(1) |
 *   scriptSig(scriptlen) | nSequence(4) | vout count(1) | value(8) |
 *   spk len(1) | spk(1) | nLockTime(4)
 * Returns the length; the caller owns nothing (static buffer). */
static int build_coinbase(unsigned char *buf, int scriptlen, uint32_t nsequence,
			  uint32_t nlocktime)
{
	int ofs = 0, i;

	memcpy(buf, "\x01\x00\x00\x00", 4);		/* nVersion 1 */
	ofs = 4;
	buf[ofs++] = 0x01;				/* one input */
	memset(buf + ofs, 0, 32);			/* null prevout hash */
	ofs += 32;
	memset(buf + ofs, 0xff, 4);			/* prevout index */
	ofs += 4;
	buf[ofs++] = (unsigned char)scriptlen;		/* scriptSig length */
	for (i = 0; i < scriptlen; i++)
		buf[ofs++] = 0xaa;
	buf[ofs++] = nsequence & 0xff;
	buf[ofs++] = (nsequence >> 8) & 0xff;
	buf[ofs++] = (nsequence >> 16) & 0xff;
	buf[ofs++] = (nsequence >> 24) & 0xff;
	buf[ofs++] = 0x01;				/* one output */
	memset(buf + ofs, 0, 8);			/* value */
	ofs += 8;
	buf[ofs++] = 0x01;				/* scriptPubKey length */
	buf[ofs++] = 0x51;				/* OP_1 */
	buf[ofs++] = nlocktime & 0xff;
	buf[ofs++] = (nlocktime >> 8) & 0xff;
	buf[ofs++] = (nlocktime >> 16) & 0xff;
	buf[ofs++] = (nlocktime >> 24) & 0xff;
	return ofs;
}

static const char *fname(coinbase_final_t f)
{
	switch (f) {
	case CBF_FINAL:		return "FINAL";
	case CBF_NONFINAL:	return "NONFINAL";
	case CBF_UNKNOWN:	return "UNKNOWN";
	case CBF_MALFORMED:	return "MALFORMED";
	}
	return "?";
}

static void check(const char *what, coinbase_final_t got, coinbase_final_t want)
{
	if (got == want) {
		printf("ok   %-58s %s\n", what, fname(got));
	} else {
		printf("FAIL %-58s got %s want %s\n", what, fname(got), fname(want));
		failures++;
	}
}

/* One row of the finality decision table. */
static void row(const char *what, uint32_t nsequence, uint32_t nlocktime,
		int64_t height, coinbase_final_t want)
{
	unsigned char buf[256];
	uint32_t seq = 0, lt = 0;
	int len = build_coinbase(buf, 40, nsequence, nlocktime);
	coinbase_final_t got = coinbase_finality(buf, len, height, &seq, &lt);

	check(what, got, want);
	/* The reported values are what the LOGEMERG prints, so they have to
	 * be right or the diagnostic is worse than none. */
	if (got != CBF_MALFORMED && (seq != nsequence || lt != nlocktime)) {
		printf("FAIL %-58s reported nSequence %08x nLockTime %u, built %08x %u\n",
		       what, seq, lt, nsequence, nlocktime);
		failures++;
	}
}

int main(void)
{
	unsigned char buf[256];
	int len;

	printf("== Finality decision table (height 800000) ==\n");
	/* nSequence final: locktime is unenforceable whatever it holds. */
	row("0xffffffff / 0            -- what this pool emits today",
	    0xffffffff, 0, 800000, CBF_FINAL);
	row("0xffffffff / height-1     -- timelock disabled by final sequence",
	    0xffffffff, 799999, 800000, CBF_FINAL);
	row("0xffffffff / height       -- still disabled by final sequence",
	    0xffffffff, 800000, 800000, CBF_FINAL);
	/* The 4.2% of BCH blocks that stuff a ~2032 timestamp into nLockTime
	 * as an extranonce are safe only because nSequence stays final. */
	row("0xffffffff / 1956528000   -- 2032 timestamp extranonce, safe",
	    0xffffffff, 1956528000u, 800000, CBF_FINAL);

	/* nSequence non-final: the locktime now bites. */
	row("0xfeffffff / 0            -- zero locktime is always final",
	    0xfeffffff, 0, 800000, CBF_FINAL);
	row("0xfeffffff / height-1     -- locktime in the past, final",
	    0xfeffffff, 799999, 800000, CBF_FINAL);
	row("0xfeffffff / height       -- THE INVALID BLOCK, must be caught",
	    0xfeffffff, 800000, 800000, CBF_NONFINAL);
	row("0xfeffffff / height+1     -- also non-final",
	    0xfeffffff, 800001, 800000, CBF_NONFINAL);
	/* Upstream's BIP54 pair, which every rebase re-introduces.
	 *
	 * Upstream writes memcpy(..., "\xff\xff\xff\xfe", 4). On the wire
	 * that is ff ff ff fe, and read back as the little-endian uint32
	 * nSequence is 0xFEFFFFFF -- NOT 0xFFFFFFFE. The two differ as
	 * inputs, and the distinction is invisible for 0xffffffff because
	 * that value is byte-order-symmetric, which is how the swapped
	 * spelling survived into PR #20's description. All 46 blocks found
	 * on chain running upstream's change read sequence 4278190079 =
	 * 0xfeffffff. Assert the real value, and the swapped one beside it
	 * so nobody re-derives the confusion from this file. */
	row("0xfeffffff / height-1     -- upstream's REAL BIP54 pair, final",
	    0xfeffffff, 799999, 800000, CBF_FINAL);
	row("0xfffffffe / height-1     -- the byte-swapped misreading of it",
	    0xfffffffe, 799999, 800000, CBF_FINAL);
	/* The whole reason this file exists: upstream's pair with the
	 * locktime drifted up by one. nLockTime = height-1 passes finality by
	 * exactly one, so any off-by-one, stale workbase reused across a
	 * height bump, or height-tracking bug turns a solved block into
	 * bad-txns-nonfinal and loses ~3.13 BCH. */
	row("0xfeffffff / height   <-- upstream's pair, drifted by ONE",
	    0xfeffffff, 800000, 800000, CBF_NONFINAL);
	/* Timestamp locktime needs median-time-past, which the coinbase does
	 * not carry, so it must not be reported as either. */
	row("0xfeffffff / 1956528000   -- time locktime, cannot be decided",
	    0xfeffffff, 1956528000u, 800000, CBF_UNKNOWN);
	/* Exactly on the height/time boundary. */
	row("0xfeffffff / 499999999    -- last height-based value",
	    0xfeffffff, 499999999u, 800000, CBF_NONFINAL);
	row("0xfeffffff / 500000000    -- first timestamp value",
	    0xfeffffff, 500000000u, 800000, CBF_UNKNOWN);

	printf("== malformed buffers ==\n");
	check("NULL buffer", coinbase_finality(NULL, 64, 800000, NULL, NULL),
	      CBF_MALFORMED);
	len = build_coinbase(buf, 40, 0xfeffffff, 800000);
	check("cblen 0", coinbase_finality(buf, 0, 800000, NULL, NULL),
	      CBF_MALFORMED);
	check("cblen 49 (below the 50 byte floor)",
	      coinbase_finality(buf, 49, 800000, NULL, NULL), CBF_MALFORMED);
	check("truncated below its own scriptSig length",
	      coinbase_finality(buf, 60, 800000, NULL, NULL), CBF_MALFORMED);
	buf[41] = 0xfd;
	check("multi-byte scriptSig varint",
	      coinbase_finality(buf, len, 800000, NULL, NULL), CBF_MALFORMED);

	printf("== scriptSig length is honoured, not assumed ==\n");
	/* A different scriptSig length moves nSequence; if the offset were
	 * hardcoded these would read output bytes instead. */
	len = build_coinbase(buf, 4, 0xfeffffff, 800000);
	check("4 byte scriptSig, non-final",
	      coinbase_finality(buf, len, 800000, NULL, NULL), CBF_NONFINAL);
	len = build_coinbase(buf, 100, 0xfeffffff, 800000);
	check("100 byte scriptSig (BIP34 max), non-final",
	      coinbase_finality(buf, len, 800000, NULL, NULL), CBF_NONFINAL);
	len = build_coinbase(buf, 100, 0xffffffff, 800000);
	check("100 byte scriptSig, final sequence",
	      coinbase_finality(buf, len, 800000, NULL, NULL), CBF_FINAL);

	if (failures) {
		printf("\n%d FAILURE(S)\n", failures);
		return 1;
	}
	printf("\nall coinbase finality vectors passed\n");
	return 0;
}
