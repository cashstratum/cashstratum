/*
 * Coinbase finality check. See coinbase_final.h for why this exists.
 *
 * Distributed under the GPL v3. See COPYING for more details.
 */

#include <string.h>

#include "coinbase_final.h"

/* Bitcoin's LOCKTIME_THRESHOLD: below this an nLockTime is a block height,
 * at or above it a UNIX timestamp. */
#define LOCKTIME_THRESHOLD 500000000U
#define SEQUENCE_FINAL     0xffffffffU

/* Fixed prefix of every coinbase: nVersion(4) + vin count(1) + null prevout
 * hash(32) + prevout index(4). The scriptSig length varint follows at [41]
 * and the scriptSig itself at [42]. */
#define COINB_SCRIPTLEN_OFS 41
#define COINB_SCRIPT_OFS    42

static uint32_t le32_at(const unsigned char *p)
{
	return (uint32_t)p[0] | ((uint32_t)p[1] << 8) |
	       ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}

coinbase_final_t coinbase_finality(const unsigned char *coinbase, int cblen,
				   int64_t height, uint32_t *nsequence,
				   uint32_t *nlocktime)
{
	uint32_t seq, locktime;
	int scriptlen, seq_ofs;

	if (nsequence)
		*nsequence = 0;
	if (nlocktime)
		*nlocktime = 0;

	/* Need at least the fixed prefix, a scriptSig length byte, an
	 * nSequence and an nLockTime to read anything at all. */
	if (!coinbase || cblen < COINB_SCRIPT_OFS + 8)
		return CBF_MALFORMED;

	/* generate_coinbase() always writes the scriptSig length as a single
	 * byte (BIP34 caps the coinbase scriptSig at 100 bytes, and the byte
	 * is assigned directly to coinb1bin[41]), so a multi-byte varint here
	 * means the buffer is not the coinbase this function was written for
	 * and its nSequence offset cannot be trusted. */
	scriptlen = coinbase[COINB_SCRIPTLEN_OFS];
	if (scriptlen >= 0xfd)
		return CBF_MALFORMED;

	seq_ofs = COINB_SCRIPT_OFS + scriptlen;
	/* nSequence(4) and nLockTime(4) must still fit, or the scriptSig
	 * length byte disagrees with the buffer. Deliberately 8, not 9: a
	 * real coinbase also carries an output count and at least one output
	 * between them, but this predicate reads only those two fields and
	 * must not reject a buffer it can in fact answer for. */
	if (seq_ofs + 8 > cblen)
		return CBF_MALFORMED;

	seq = le32_at(coinbase + seq_ofs);
	locktime = le32_at(coinbase + cblen - 4);
	if (nsequence)
		*nsequence = seq;
	if (nlocktime)
		*nlocktime = locktime;

	/* Bitcoin's IsFinalTx, specialised to the coinbase's single input:
	 *   nLockTime == 0                         -> final
	 *   nLockTime < (height | block time)      -> final
	 *   every input nSequence == 0xffffffff    -> final
	 * so only a non-final nSequence AND an effective nLockTime can make
	 * the transaction non-final. */
	if (!locktime)
		return CBF_FINAL;
	if (seq == SEQUENCE_FINAL)
		return CBF_FINAL;
	if (locktime < LOCKTIME_THRESHOLD)
		return (int64_t)locktime < height ? CBF_FINAL : CBF_NONFINAL;

	/* Timestamp-based lock time with a non-final sequence. Finality then
	 * turns on the median-time-past of the block being built, which is
	 * not knowable from the coinbase alone. */
	return CBF_UNKNOWN;
}
