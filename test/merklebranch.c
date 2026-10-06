/* Exercise CashStratum's actual template branch builder against full trees.
 * GPLv3; see COPYING. */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "sha2.h"
#include "merkle.h"

static void hash_pair(const unsigned char *pair, unsigned char *out)
{
	unsigned char first[32];
	sha256(pair, 64, first);
	sha256(first, 32, out);
}

static int check(size_t transactions)
{
	size_t leaves = transactions + 1, n, i;
	unsigned char *tree = calloc(leaves + 1, 32);
	unsigned char *work = calloc(leaves + 1, 32);
	unsigned char pair[64], coinbase[32];
	char branch[GENWORK_MAX_MERKLE_DEPTH][32];
	int depth, expected = 0, result = 1;

	if (!tree || !work)
		abort();
	for (i = 0; i < leaves; i++) {
		unsigned char seed[8];
		for (size_t j = 0; j < sizeof(seed); j++)
			seed[j] = ((uint64_t)i >> (8 * j)) & 255;
		sha256(seed, sizeof(seed), tree + i * 32);
	}
	memcpy(coinbase, tree, 32);
	memcpy(work, tree, leaves * 32);
	memset(work, 0, 32); /* Production does not know the miner's coinbase. */
	if (!cashstratum_merkle_branch(work, leaves, branch, &depth))
		goto out;
	for (n = leaves; n > 1; n = (n + 1) / 2)
		expected++;
	if (depth != expected)
		goto out;
	/* Independently hash the entire tree, including the concrete coinbase. */
	for (n = leaves; n > 1; n /= 2) {
		if (n % 2) {
			memcpy(tree + n * 32, tree + (n - 1) * 32, 32);
			n++;
		}
		for (i = 0; i < n; i += 2)
			hash_pair(tree + i * 32, tree + i / 2 * 32);
	}
	memcpy(pair, coinbase, 32);
	for (int level = 0; level < depth; level++) {
		memcpy(pair + 32, branch[level], 32);
		hash_pair(pair, pair);
	}
	result = memcmp(tree, pair, 32) != 0;
out:
	if (result)
		fprintf(stderr, "Merkle root mismatch for %zu transactions\n", transactions);
	free(work);
	free(tree);
	return result;
}

int main(void)
{
	const size_t counts[] = {0, 1, 2, 3, 7, 8, 65534, 65535, 65536, 131071, 131072, 300000};
	unsigned char dummy[64] = {0};
	char branch[GENWORK_MAX_MERKLE_DEPTH][32];
	int depth, failures = 0;

	for (size_t i = 0; i < sizeof(counts) / sizeof(counts[0]); i++)
		failures += check(counts[i]);
	if (cashstratum_merkle_branch(dummy, 0, branch, &depth) ||
	    cashstratum_merkle_branch(dummy, SIZE_MAX, branch, &depth))
		failures++;
#if SIZE_MAX > UINT32_MAX
	if (cashstratum_merkle_branch(dummy, (size_t)UINT32_MAX + 2, branch, &depth))
		failures++;
#endif
	if (!failures)
		puts("CashStratum merkle branches: small, odd, and BCH large templates passed");
	return failures ? EXIT_FAILURE : EXIT_SUCCESS;
}
