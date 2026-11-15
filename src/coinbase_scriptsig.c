/*
 * Coinbase scriptSig construction. See coinbase_scriptsig.h for the layout
 * and why it is ordered that way.
 *
 * Distributed under the GPL v3. See COPYING for more details.
 */

#include <string.h>

#include "coinbase_scriptsig.h"

/* Declared in libckpool.h, which pulls in platform headers this file does
 * not need. */
int ser_number(unsigned char *s, int32_t val);

int ser_bip34_height(uint8_t *buf, uint32_t height)
{
	if (height == 0) {
		buf[0] = 0x00;                  /* OP_0 */
		return 1;
	}
	if (height <= 16) {
		buf[0] = 0x50 + (uint8_t)height; /* OP_1 (0x51) … OP_16 (0x60) */
		return 1;
	}

	/* General case: shortest little-endian bytes + push length */
	uint8_t tmp[5];
	int nlen = 0;
	uint32_t h = height;
	do {
		tmp[nlen++] = h & 0xff;
		h >>= 8;
	} while (h);

	if (tmp[nlen-1] & 0x80)
		tmp[nlen++] = 0x00;             /* sign byte for positive number */

	buf[0] = (uint8_t)nlen;             /* push length */
	memcpy(buf + 1, tmp, nlen);
	return nlen + 1;
}

/* Append bytes at *ofs if they fit in cap. */
static bool put(unsigned char *out, size_t cap, size_t *ofs,
		const void *src, size_t len)
{
	if (len > cap - *ofs)
		return false;
	if (len)
		memcpy(out + *ofs, src, len);
	*ofs += len;
	return true;
}

/* ser_number() always stores 5 bytes whatever length it reports, so it
 * serialises into scratch space and only the bytes it uses are appended. */
static bool put_number(unsigned char *out, size_t cap, size_t *ofs,
		       int32_t val)
{
	unsigned char tmp[8];
	int len = ser_number(tmp, val);

	return put(out, cap, ofs, tmp, len);
}

int coinbase_scriptsig_prefix(unsigned char *out, size_t cap,
			      const unsigned char *height_bytes, int height_len,
			      const char *text, size_t textlen,
			      const unsigned char *flags, size_t flagslen,
			      const coinbase_stamp_t *stamp,
			      int enonce_len, int *scriptlen)
{
	unsigned char byte;
	size_t ofs = 0;

	if (!out || !scriptlen || !height_bytes || height_len < 1)
		return -1;
	if (flagslen > 0xff || enonce_len < 0 || enonce_len > 75)
		return -1;
	if (!text)
		textlen = 0;
	if (!flags)
		flagslen = 0;

	/* BIP34 height first */
	if (!put(out, cap, &ofs, height_bytes, height_len))
		return -1;

	/* Operator text, raw */
	if (!put(out, cap, &ofs, text, textlen))
		return -1;

	/* Coinbase flags with their length byte */
	byte = (unsigned char)flagslen;
	if (!put(out, cap, &ofs, &byte, 1) ||
	    !put(out, cap, &ofs, flags, flagslen))
		return -1;

	if (stamp) {
		if (!put_number(out, cap, &ofs, stamp->tv_sec) ||
		    !put_number(out, cap, &ofs, stamp->tv_nsec))
			return -1;
	}

	/* Push length of the extranonce the miner appends */
	byte = (unsigned char)enonce_len;
	if (!put(out, cap, &ofs, &byte, 1))
		return -1;

	*scriptlen = (int)ofs + enonce_len;
	return (int)ofs;
}
