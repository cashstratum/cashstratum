/*
 * Coinbase scriptSig construction.
 *
 * The coinbase input's scriptSig is the part of coinb1 that follows the
 * fixed 41-byte transaction prefix and the one-byte scriptSig length at
 * offset 41. Its layout is:
 *
 *   height | operator text | flags | [tv_sec | tv_nsec] | extranonce
 *
 *   height       BIP34 block height. It must come first.
 *   text         the configured btcsig, written raw with no push-length
 *                byte. Block explorers and miner screens render the whole
 *                scriptSig as text, so the operator text comes right after
 *                the height and no length byte renders in front of it.
 *   flags        one length byte followed by the node's coinbase flags.
 *   tv_sec       optional serialised wall-clock seconds and nanoseconds.
 *   tv_nsec      They make coinb1 differ between workbases built from the
 *                same template.
 *   extranonce   one push-length byte; the extranonce1 and extranonce2
 *                bytes the miner fills in follow it. coinb1 ends at that
 *                byte and coinb2 starts directly at the input's nSequence,
 *                so nothing may follow the extranonce in the scriptSig.
 *
 * Nothing in consensus executes the coinbase scriptSig; it checks the BIP34
 * height and a total length of 2 to 100 bytes.
 *
 * Distributed under the GPL v3. See COPYING for more details.
 */

#ifndef COINBASE_SCRIPTSIG_H
#define COINBASE_SCRIPTSIG_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* Consensus bounds on the length of a coinbase scriptSig. */
#define COINBASE_SCRIPTSIG_MIN 2
#define COINBASE_SCRIPTSIG_MAX 100

/* Room for the largest prefix the daemon can build: 6 height + 38 text +
 * 1 + 32 flags + 5 tv_sec + 5 tv_nsec + 1 extranonce length = 88 bytes,
 * rounded up. Callers size their buffer from this. */
#define COINBASE_SCRIPTSIG_PREFIX_CAP 96

/* The two optional time pushes. */
typedef struct coinbase_stamp {
	int32_t tv_sec;
	int32_t tv_nsec;
} coinbase_stamp_t;

/* Minimal CScriptNum encoding of a block height as Bitcoin Core expects:
 * OP_0, OP_1..OP_16, or a push of the shortest little-endian bytes. Writes
 * at most 6 bytes to buf and returns the number used. */
int ser_bip34_height(uint8_t *buf, uint32_t height);

/* Write the scriptSig from its first byte up to and including the extranonce
 * push-length byte.
 *
 * height_bytes/height_len  the already serialised height push.
 * text/textlen             operator text, copied raw; NULL or 0 for none.
 * flags/flagslen           coinbase flag bytes; NULL or 0 for none.
 *                          flagslen must fit the single length byte.
 * stamp                    the time pushes, each serialised with
 *                          ser_number(); NULL leaves them out.
 * enonce_len               extranonce1 + extranonce2 length, 0..75.
 *
 * Returns the number of bytes written to out, or -1 if an input is out of
 * range or the result would not fit in cap. On success *scriptlen is the
 * total scriptSig length (the bytes written plus enonce_len), which is the
 * value of the scriptSig length byte. The caller checks it against
 * COINBASE_SCRIPTSIG_MAX.
 *
 * Pure apart from ser_number(): no globals, no clock, no allocation, no
 * logging. Writes only within [out, out + cap).
 */
int coinbase_scriptsig_prefix(unsigned char *out, size_t cap,
			      const unsigned char *height_bytes, int height_len,
			      const char *text, size_t textlen,
			      const unsigned char *flags, size_t flagslen,
			      const coinbase_stamp_t *stamp,
			      int enonce_len, int *scriptlen);

#endif /* COINBASE_SCRIPTSIG_H */
