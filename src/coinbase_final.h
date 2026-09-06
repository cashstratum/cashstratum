/*
 * Coinbase finality check.
 *
 * A block is invalid unless every transaction in it is final, coinbase
 * included -- bitcoind rejects a non-final one with "bad-txns-nonfinal" and
 * the entire reward is lost. generate_coinbase() currently emits nSequence
 * 0xffffffff and nLockTime 0, which is final unconditionally, so this cannot
 * fire today. It exists because the combination that breaks it is one
 * careless edit away, it re-checks itself on every upstream rebase (each of
 * which re-introduces upstream's BIP54 nSequence/nLockTime pair), and the
 * failure it guards costs a whole block.
 *
 * Distributed under the GPL v3. See COPYING for more details.
 */

#ifndef COINBASE_FINAL_H
#define COINBASE_FINAL_H

#include <stdint.h>

typedef enum {
	/* Provably final: safe to submit. */
	CBF_FINAL = 0,
	/* Provably NOT final: height-based nLockTime >= the block's height
	 * combined with a non-final nSequence. bitcoind will reject this. */
	CBF_NONFINAL,
	/* Non-final nSequence with a timestamp-based nLockTime (>= 5e8).
	 * Finality then depends on the median-time-past of the block being
	 * built, which is not available here, so this is neither confirmed
	 * nor cleared. */
	CBF_UNKNOWN,
	/* The buffer is too short, or its scriptSig length is not the
	 * single-byte varint generate_coinbase() emits, so the nSequence
	 * offset cannot be located. */
	CBF_MALFORMED
} coinbase_final_t;

/* Classify the finality of a serialised legacy coinbase transaction.
 *
 * coinbase/cblen is the assembled coinbase exactly as it is hashed into the
 * merkle root and submitted (version || vin || vout || nLockTime, no witness
 * marker). height is the height of the block being solved.
 *
 * Pure: no locks, no globals, no allocation, no logging. Reads only within
 * [coinbase, coinbase + cblen).
 */
coinbase_final_t coinbase_finality(const unsigned char *coinbase, int cblen,
				   int64_t height, uint32_t *nsequence,
				   uint32_t *nlocktime);

#endif /* COINBASE_FINAL_H */
