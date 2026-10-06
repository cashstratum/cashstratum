/* CashStratum BCH merkle branches. GPLv3; see COPYING. */
#include <stdint.h>
#include <string.h>
#include "sha2.h"
#include "merkle.h"

bool cashstratum_merkle_branch(unsigned char *hashes, size_t leaves,
			      char branch[GENWORK_MAX_MERKLE_DEPTH][32], int *depth)
{
	size_t i, n = leaves;
	int required = 0;

	if (!hashes || !branch || !depth || !leaves || leaves > SIZE_MAX / 32 - 1)
		return false;
	while (n > 1) {
		n = n / 2 + n % 2;
		required++;
	}
	if (required > GENWORK_MAX_MERKLE_DEPTH)
		return false;
	*depth = 0;
	while (leaves > 1) {
		memcpy(branch[(*depth)++], hashes + 32, 32);
		if (leaves % 2) {
			memcpy(hashes + leaves * 32, hashes + (leaves - 1) * 32, 32);
			leaves++;
		}
		/* The unknown coinbase path is assembled when a miner submits.
		 * Hash only sibling subtrees here. */
		for (i = 2; i < leaves; i += 2) {
			unsigned char first[32];
			sha256(hashes + i * 32, 64, first);
			sha256(first, 32, hashes + (i / 2) * 32);
		}
		leaves /= 2;
	}
	return true;
}
