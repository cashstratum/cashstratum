/* CashStratum BCH merkle branches. GPLv3; see COPYING. */
#ifndef CASHSTRATUM_MERKLE_H
#define CASHSTRATUM_MERKLE_H
#include <stdbool.h>
#include <stddef.h>

#define GENWORK_MAX_MERKLE_DEPTH 32

/* hashes contains leaves * 32 bytes plus 32 bytes for odd-node padding.
 * Leaf zero is the coinbase placeholder. The workspace is overwritten. */
bool cashstratum_merkle_branch(unsigned char *hashes, size_t leaves,
			      char branch[GENWORK_MAX_MERKLE_DEPTH][32],
                              int *depth);
#endif
