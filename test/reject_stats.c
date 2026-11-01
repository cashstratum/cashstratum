/*
 * Copyright 2026 Con Kolivas
 *
 * This program is free software; you can redistribute it and/or modify it
 * under the terms of the GNU General Public License as published by the Free
 * Software Foundation; either version 3 of the License, or (at your option)
 * any later version.  See COPYING for more details.
 *
 * Reproduce huge rejected-difficulty batches and totals without a live miner,
 * and cap rejects at the highest assigned difficulty met by an accepted share.
 * The pool counter and the per-worker/per-user counters must credit the same
 * capped difficulty.
 */
#include "config.h"
#include <assert.h>
#include <math.h>
#include <stdio.h>

#include "../src/stratifier.c"

static void test_client_rejects(void)
{
	stratum_instance_t client = {0}, other = {0}, highdiff = {0}, sv2 = {0};
	worker_instance_t worker = {0};
	pool_stats_t stats = {0};
	int i;

	client.start_diff = 1024;
	client.worker_instance = &worker;
	client.diff = client.suggest_diff = 1000000000000000000LL;
	for (i = 0; i < 20; i++)
		account_client_share(&stats, &client, client.diff, false);
	assert(stats.unaccounted_rejects == 20 * 1024);
	assert(!client.max_accepted_diff);
	assert(!stats.unaccounted_shares && !stats.unaccounted_diff_shares);

	/* A timestamp set by stale work must not unlock suggested difficulty. */
	client.first_share.tv_sec = 1;
	account_client_share(&stats, &client, client.diff, false);
	assert(stats.unaccounted_rejects == 21 * 1024);
	assert(!client.max_accepted_diff);

	/* Accept normal work, then suggest an enormous diff and submit rejects. */
	client.diff = 4096;
	account_client_share(&stats, &client, client.diff, true);
	assert(client.max_accepted_diff == 4096);
	assert(stats.unaccounted_shares == 1 && stats.unaccounted_diff_shares == 4096);
	client.diff = client.suggest_diff = 1000000000000000000LL;
	for (i = 0; i < 20; i++)
		account_client_share(&stats, &client, client.diff, false);
	assert(stats.unaccounted_rejects == 21 * 1024 + 20 * 4096);
	assert(client.max_accepted_diff == 4096);

	/* A lucky high PoW diff does not prove the higher assigned difficulty. */
	client.best_diff = 1e18;
	account_client_share(&stats, &client, client.diff, false);
	assert(stats.unaccounted_rejects == 21 * 1024 + 21 * 4096);

	/* Lower job difficulty is counted as-is, including old-diff submissions. */
	stats.unaccounted_rejects = 0;
	account_client_share(&stats, &client, 512, false);
	assert(stats.unaccounted_rejects == 512);

	/* Only acceptance at a higher assigned diff raises the cap. */
	account_client_share(&stats, &client, 8192, true);
	assert(client.max_accepted_diff == 8192);
	assert(stats.unaccounted_shares == 2 && stats.unaccounted_diff_shares == 12288);
	account_client_share(&stats, &client, 1e18, false);
	assert(stats.unaccounted_rejects == 512 + 8192);
	account_client_share(&stats, &client, 256, true);
	assert(client.max_accepted_diff == 8192);
	assert(stats.unaccounted_shares == 3 && stats.unaccounted_diff_shares == 12544);

	/* Pool stats resets must not erase this connection's accepted history. */
	memset(&stats, 0, sizeof(stats));
	account_client_share(&stats, &client, 16384, false);
	assert(stats.unaccounted_rejects == 8192);

	/* Another connection, even for the same worker, starts unproven. */
	other.start_diff = 1024;
	other.worker_instance = &worker;
	account_client_share(&stats, &other, 1e18, false);
	assert(stats.unaccounted_rejects == 8192 + 1024);
	assert(!other.max_accepted_diff);
	account_client_share(&stats, &other, 256, true);
	account_client_share(&stats, &other, 1e18, false);
	assert(stats.unaccounted_rejects == 8192 + 1024 + 256);

	/* Preserve each port's initial difficulty, not just global startdiff. */
	highdiff.start_diff = 1000000;
	stats.unaccounted_rejects = 0;
	account_client_share(&stats, &highdiff, 1e18, false);
	assert(stats.unaccounted_rejects == 1000000);

	/* SV2 negotiated difficulty obeys the same cap, including after a share. */
	sv2.start_diff = 1024;
	sv2.sv2 = true;
	sv2.diff = 1000000000000000LL;
	stats.unaccounted_rejects = 0;
	account_client_share(&stats, &sv2, sv2.diff, false);
	assert(stats.unaccounted_rejects == 1024);
	account_client_share(&stats, &sv2, 2048, true);
	account_client_share(&stats, &sv2, sv2.diff, false);
	assert(stats.unaccounted_rejects == 1024 + 2048);

	/* Even a client that has proved enormous difficulty cannot overflow. */
	account_client_share(&stats, &client, 1e18, true);
	for (i = 0; i < 20; i++)
		account_client_share(&stats, &client, 1e18, false);
	assert(stats.unaccounted_rejects == INT64_MAX);
}

/* The per-worker and per-user counters credit exactly what the pool counter
 * does, including rejects that never reach the pool counter. */
static void test_entity_rejects(void)
{
	stratum_instance_t client = {0}, anon = {0};
	worker_instance_t worker = {0};
	user_instance_t user = {0};
	pool_stats_t stats = {0};
	int i;

	client.start_diff = 1024;
	client.worker_instance = &worker;
	client.user_instance = &user;
	client.diff = 1000000000000000000LL;

	/* Unproven connection: credited at start_diff for both counters. */
	for (i = 0; i < 20; i++) {
		account_client_share(&stats, &client, client.diff, false);
		account_reject(&client, client.diff, RB_STALE, "Stale");
	}
	assert(stats.unaccounted_rejects == 20 * 1024);
	assert(worker.rstats.rejected == 20 * 1024);
	assert(user.rstats.rejected == 20 * 1024);
	assert(worker.rstats.count == 20 && user.rstats.count == 20);
	assert(worker.rstats.buckets[RB_STALE] == 20);

	/* An early reject never reaches the pool counter but is still capped. */
	account_reject(&client, client.diff, RB_MALFORMED, "Invalid nonce");
	assert(stats.unaccounted_rejects == 20 * 1024);
	assert(worker.rstats.rejected == 21 * 1024);
	assert(user.rstats.rejected == 21 * 1024);
	assert(worker.rstats.buckets[RB_MALFORMED] == 1);

	/* Accepted work raises the cap for both counters alike. */
	account_client_share(&stats, &client, 4096, true);
	stats.unaccounted_rejects = 0;
	worker.rstats.rejected = user.rstats.rejected = 0;
	account_client_share(&stats, &client, client.diff, false);
	account_reject(&client, client.diff, RB_LOWDIFF, "Above target");
	assert(stats.unaccounted_rejects == 4096);
	assert(worker.rstats.rejected == 4096 && user.rstats.rejected == 4096);

	/* Lower job difficulty is credited as-is. */
	account_reject(&client, 512, RB_STALE, "Stale");
	assert(worker.rstats.rejected == 4096 + 512);

	/* Per-entity totals saturate rather than overflow. */
	account_client_share(&stats, &client, 1e18, true);
	for (i = 0; i < 20; i++)
		account_reject(&client, 1e18, RB_STALE, "Stale");
	assert(worker.rstats.rejected == INT64_MAX);
	assert(user.rstats.rejected == INT64_MAX);

	/* A client with no worker or user is counted on neither. */
	anon.start_diff = 1024;
	account_reject(&anon, 1e18, RB_WORKER, "Worker mismatch");
	assert(credited_reject_diff(&anon, 1e18) == 1024);
}

/* A block-solving share (sdiff >= network_diff) updates best_ever on user and
 * worker, but must not overwrite current-round best_diff on client, worker,
 * user, or pool stats after block_solve() has reset them to zero. */
static void test_best_diff_block_solve_guard(void)
{
	sdata_t sdata = {0};
	workbase_t wb = {0};
	user_instance_t user = {0};
	worker_instance_t worker = {0};
	stratum_instance_t client = {0};

	mutex_init(&sdata.stats_lock);
	wb.network_diff = 1000.0;
	sdata.current_workbase = &wb;
	client.user_instance = &user;
	client.worker_instance = &worker;

	/* Normal sub-block share updates all current-round best_diff and best_ever. */
	record_best_diff(&sdata, &user, &worker, 500.0, &client, NULL, NULL);
	assert(client.best_diff == 500.0);
	assert(user.best_ever == 500.0 && worker.best_ever == 500.0);
	assert(user.best_diff == 500.0 && worker.best_diff == 500.0);
	assert(sdata.stats.best_diff == 500.0);

	/* Simulate block_solve() resetting current-round best_diff to zero, followed
	 * immediately by the caller invoking check_best_diff() with the winning share. */
	reset_pool_bestshares(&sdata);
	client.best_diff = user.best_diff = worker.best_diff = 0.0;
	record_best_diff(&sdata, &user, &worker, 2500.0, &client, NULL, NULL);
	assert(user.best_ever == 2500.0 && worker.best_ever == 2500.0);
	assert(client.best_diff == 0.0);
	assert(user.best_diff == 0.0 && worker.best_diff == 0.0);
	assert(sdata.stats.best_diff == 0.0);

	/* Subsequent normal share in the new round records cleanly. */
	record_best_diff(&sdata, &user, &worker, 120.0, &client, NULL, NULL);
	assert(user.best_ever == 2500.0 && worker.best_ever == 2500.0);
	assert(client.best_diff == 120.0);
	assert(user.best_diff == 120.0 && worker.best_diff == 120.0);
	assert(sdata.stats.best_diff == 120.0);
}

int main(void)
{
	int64_t pending = 0, total = 0;
	int i;

	test_client_rejects();
	test_entity_rejects();
	test_best_diff_block_solve_guard();
	assert(add_reject_diff(100, 42) == 142);
	assert(add_reject_diff(100, 42.5) == 142);
	assert(add_reject_diff(100, 0) == 100);
	assert(add_reject_diff(100, -1) == 100);
	assert(add_reject_diff(100, NAN) == 100);
	assert(add_reject_diff(100, INFINITY) == INT64_MAX);
	assert(add_reject_diff(0, 0x1p63) == INT64_MAX);
	assert(add_reject_diff(0, nextafter(0x1p63, 0)) == INT64_MAX - 1023);
	assert(add_reject_diff(INT64_MAX, 1) == INT64_MAX);
	assert(add_reject_diff(INT64_MAX - 1, 1) == INT64_MAX);
	assert(add_reject_diff(INT64_MAX - 1, 2) == INT64_MAX);
	assert(add_rejects(INT64_MAX - 1, 0) == INT64_MAX - 1);
	assert(add_rejects(0, INT64_MAX) == INT64_MAX);
	assert(add_rejects(INT64_MAX, INT64_MAX) == INT64_MAX);
	assert(add_rejects(0, -1) == 0);
	assert(add_rejects(INT64_MIN, 42) == 42);

	/* Many rejections within one stats interval. */
	for (i = 0; i < 20; i++)
		pending = add_reject_diff(pending, 1e18);
	assert(pending == INT64_MAX);

	/* The same submissions spread across stats intervals. */
	for (i = 0; i < 20; i++) {
		pending = add_reject_diff(0, 1e18);
		total = add_rejects(total, pending);
	}
	assert(total == INT64_MAX);
	assert(add_rejects(total, 0) == INT64_MAX);

	/* A block reset starts a fresh counter. */
	total = 0;
	assert(add_rejects(total, 42) == 42);
	puts("reject_stats: all OK");
	return 0;
}
