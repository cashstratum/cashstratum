/*
 * Copyright 2026 Con Kolivas
 *
 * This program is free software; you can redistribute it and/or modify it
 * under the terms of the GNU General Public License as published by the Free
 * Software Foundation; either version 3 of the License, or (at your option)
 * any later version.  See COPYING for more details.
 *
 * Password difficulty and vardiff. A valid d=/diff= password token pins the
 * client's difficulty: vardiff never re-targets it and suggest_difficulty is
 * ignored. Clients without one keep the vardiff target computed by
 * vardiff_target(), whose current behaviour is recorded here, including the
 * one-way ratchet a floor client has today.
 */
#include "config.h"
#include <assert.h>
#include <math.h>
#include <stdarg.h>
#include <stdio.h>

#include "../src/stratifier.c"

/* The daemon defines these in ckpool.c, which this test does not link. */
ckpool_t ckpool;

static int log_count;
static char last_log[DEFLOGBUFSIZ];

void logmsg(int loglevel, const char *fmt, ...)
{
	va_list ap;

	(void)loglevel;
	va_start(ap, fmt);
	vsnprintf(last_log, sizeof(last_log), fmt, ap);
	va_end(ap);
	log_count++;
}

/* add_submit() can announce a new difficulty, which reaches the message queue
 * and the JSON encoder. A pinned client must never get that far, so these
 * stand-ins abort rather than pretend to send. */
bool _ckmsgq_add(ckmsgq_t *ckmsgq, void *data, bool head, const char *file,
		 const char *func, const int line)
{
	(void)ckmsgq; (void)data; (void)head; (void)file; (void)func; (void)line;
	abort();
}

yyjson_mut_doc *_yyjson_mut_pack(const char *file, const char *func, const int line,
				 const char *types, const char *fmt, ...)
{
	(void)file; (void)func; (void)line; (void)types; (void)fmt;
	abort();
}

void yyjson_mut_doc_free(yyjson_mut_doc *doc)
{
	(void)doc;
	abort();
}

bool unsafe_yyjson_val_pool_grow(yyjson_val_pool *pool, const yyjson_alc *alc,
				 size_t count)
{
	(void)pool; (void)alc; (void)count;
	abort();
}

#ifdef HAVE_SV2
void sv2_strat_set_instance_diff(int64_t instance_id, double diff)
{
	(void)instance_id; (void)diff;
	abort();
}
#endif

#define POOL_MINDIFF 2
#define NETDIFF 1e12

static int64_t target(double dsps, int64_t cur, int64_t floor_diff)
{
	double drr = -1;
	int64_t ret = vardiff_target(dsps, cur, floor_diff, POOL_MINDIFF, 0, NETDIFF, &drr);

	assert(drr == dsps / (double)cur);
	return ret;
}

/* (a) A floor client (suggest_diff or worker mindiff set, not pinned) is
 * raised once its rate reaches drr 0.5, and can then never come back down:
 * any drr below 0.5 leaves it where it is. This is the current behaviour. */
static void test_floor_client_ratchets(void)
{
	assert(target(80, 100, 100) == 192);	/* drr 0.8: 2.4x */
	assert(target(50, 100, 100) == 120);	/* drr 0.5 exactly */
	assert(target(45, 100, 100) == 0);	/* drr 0.45: held */
	assert(target(30, 100, 100) == 0);	/* hysteresis band */
	/* Raised well above the floor, then the rate drops: stuck. */
	assert(target(10, 400, 100) == 0);
	assert(target(1, 400, 100) == 0);
	/* The floor still bounds a raise from below. */
	assert(target(1000, 2000, 5000) == 5000);
}

/* (c) A client with no floor (password "x") takes the 3.33x path, both up and
 * down, clamped to the pool mindiff. */
static void test_no_floor_client(void)
{
	assert(target(80, 100, 0) == 266);	/* lround(266.4) */
	assert(target(1, 100, 0) == 3);		/* drr 0.01: lowered */
	assert(target(0.1, 100, 0) == POOL_MINDIFF);
	assert(target(30, 100, 0) == 0);	/* hysteresis band */
}

/* Pool maxdiff and network difficulty cap every target, and anything below
 * 1 means no change. */
static void test_target_clamps(void)
{
	double drr;

	assert(vardiff_target(1e6, 100, 0, 2, 4096, NETDIFF, &drr) == 4096);
	assert(vardiff_target(1e6, 100, 64, 2, 4096, NETDIFF, &drr) == 4096);
	/* Regtest: network difficulty is 1, so vardiff cannot rise above it. */
	assert(vardiff_target(1e6, 1, 0, 1, 0, 1, &drr) == 1);
	assert(vardiff_target(1e6, 1, 0, 0, 0, 0.5, &drr) == 0);
}

static void setup_client(stratum_instance_t *client, sdata_t *sdata, workbase_t *wb,
			 worker_instance_t *worker, user_instance_t *user)
{
	memset(sdata, 0, sizeof(*sdata));
	memset(wb, 0, sizeof(*wb));
	memset(worker, 0, sizeof(*worker));
	memset(user, 0, sizeof(*user));
	memset(client, 0, sizeof(*client));
	mutex_init(&sdata->uastats_lock);
	cklock_init(&sdata->workbase_lock);
	wb->network_diff = NETDIFF;
	sdata->current_workbase = wb;
	ckpool.sdata = sdata;
	client->sdata = sdata;
	client->worker_instance = worker;
	client->user_instance = user;
	strcpy(client->identity, "7");
	client->start_diff = 8;
}

/* (b) A pinned client is never moved by vardiff, whether its rate would raise
 * or lower it, and its vardiff window is left as it was. */
static void test_pinned_client_holds(void)
{
	stratum_instance_t client;
	worker_instance_t worker;
	user_instance_t user;
	sdata_t sdata;
	workbase_t wb;
	const double rates[] = { 1e6, 1e-3 };
	unsigned int i;
	int s;

	for (i = 0; i < sizeof(rates) / sizeof(rates[0]); i++) {
		setup_client(&client, &sdata, &wb, &worker, &user);
		client.diff = client.old_diff = client.suggest_diff = 64;
		client.pinned_diff = true;
		client.first_share.tv_sec = 1;
		client.ldc.tv_sec = 1;
		client.ssdc = 71;
		client.dsps1 = client.dsps5 = rates[i];

		/* Without the pin, these rates would re-target the client. */
		assert(target(rates[i] * 10, 64, 0) != 0);

		for (s = 0; s < 200; s++)
			add_submit(&client, 64, true, true);
		assert(client.diff == 64 && client.old_diff == 64);
		assert(client.suggest_diff == 64);
		assert(client.ssdc == 71 && client.ldc.tv_sec == 1);
		assert(worker.shares == 200 * 64 && user.shares == 200 * 64);
	}
}

/* A pinned client's suggest_difficulty leaves the floor and the difficulty
 * alone and is logged once per client. */
static void test_pinned_suggest_ignored(void)
{
	stratum_instance_t client = {0};

	strcpy(client.identity, "7");
	client.diff = client.suggest_diff = 64;
	client.pinned_diff = true;
	log_count = 0;
	ignore_pinned_suggest(&client);
	assert(log_count == 1);
	assert(!strcmp(last_log,
		       "Client 7 ignored suggest_difficulty: difficulty pinned to 64 via password parameter"));
	ignore_pinned_suggest(&client);
	ignore_pinned_suggest(&client);
	assert(log_count == 1);
	assert(client.diff == 64 && client.suggest_diff == 64);
}

/* Authorise once more on the same connection, as parse_authorise() does. */
static bool reauth(stratum_instance_t *client, const char *pass)
{
	const bool was_pinned = reset_auth_pin(client);

	apply_password_diff(client, pass);
	return was_pinned;
}

/* A re-authorisation without a valid d= leaves the pin and every trace of it:
 * the floor and difficulty go back to what the connection had before any
 * password, so the client is on plain vardiff again. */
static void test_reauth_unpins_plain(void)
{
	stratum_instance_t client = {0};

	strcpy(client.identity, "7");
	client.diff = client.old_diff = ckpool.startdiff;

	assert(!reauth(&client, "d=64"));
	assert(client.pinned_diff && client.diff == 64 && client.suggest_diff == 64);

	assert(reauth(&client, "x"));
	assert(!client.pinned_diff);
	assert(client.suggest_diff == 0);
	assert(client.diff == ckpool.startdiff && client.old_diff == ckpool.startdiff);

	/* Pinning again works, and dropping it again restores the same state. */
	assert(!reauth(&client, "d=512"));
	assert(client.pinned_diff && client.diff == 512 && client.suggest_diff == 512);
	assert(reauth(&client, "d=10M"));
	assert(!client.pinned_diff && client.suggest_diff == 0 && client.diff == ckpool.startdiff);
}

/* A rental connection keeps its useragent floor after leaving a pin, and a
 * new d= is clamped against that floor, not against the previous pin. */
static void test_reauth_unpins_rental(void)
{
	stratum_instance_t client = {0};

	strcpy(client.identity, "7");
	client.rental_diff = true;
	client.suggest_diff = client.base_suggest_diff = 500000;
	client.diff = client.old_diff = 500000;

	reauth(&client, "d=1000000");
	assert(client.pinned_diff && client.diff == 1000000 && client.suggest_diff == 1000000);

	reauth(&client, "x");
	assert(!client.pinned_diff);
	assert(client.suggest_diff == 500000 && client.diff == 500000 && client.old_diff == 500000);

	reauth(&client, "d=1000000");
	reauth(&client, "d=600000");
	assert(client.pinned_diff && client.diff == 600000 && client.suggest_diff == 600000);

	reauth(&client, "d=64");
	assert(client.pinned_diff && client.diff == 500000 && client.suggest_diff == 500000);
	reauth(&client, "");
	assert(!client.pinned_diff && client.suggest_diff == 500000 && client.diff == 500000);
}

/* A server highdiff floor comes back too, above startdiff as at connect. */
static void test_reauth_unpins_highdiff(void)
{
	stratum_instance_t client = {0};

	strcpy(client.identity, "7");
	client.suggest_diff = client.base_suggest_diff = 1024;
	client.diff = client.old_diff = 1024;

	reauth(&client, "d=64");
	assert(client.pinned_diff && client.diff == 64);
	reauth(&client, "x");
	assert(!client.pinned_diff && client.suggest_diff == 1024 && client.diff == 1024);
}

/* A connection that was never pinned is left exactly as it is, including a
 * floor it set itself with suggest_difficulty. */
static void test_reauth_unpinned_untouched(void)
{
	stratum_instance_t client = {0};

	strcpy(client.identity, "7");
	client.suggest_diff = 100;
	client.diff = 300;
	client.old_diff = 200;

	assert(!reauth(&client, "x"));
	assert(!client.pinned_diff);
	assert(client.suggest_diff == 100 && client.diff == 300 && client.old_diff == 200);
}

/* The ignored suggest_difficulty notice is once per pin, so a connection
 * pinned again logs it again. */
static void test_repin_logs_suggest_again(void)
{
	stratum_instance_t client = {0};

	strcpy(client.identity, "7");
	client.diff = client.old_diff = ckpool.startdiff;

	reauth(&client, "d=64");
	log_count = 0;
	ignore_pinned_suggest(&client);
	ignore_pinned_suggest(&client);
	assert(log_count == 1);

	reauth(&client, "x");
	reauth(&client, "d=128");
	log_count = 0;
	ignore_pinned_suggest(&client);
	ignore_pinned_suggest(&client);
	assert(log_count == 1);
	assert(!strcmp(last_log,
		       "Client 7 ignored suggest_difficulty: difficulty pinned to 128 via password parameter"));

	/* Re-pinning straight from a pin resets it as well. */
	reauth(&client, "d=256");
	log_count = 0;
	ignore_pinned_suggest(&client);
	assert(log_count == 1);
}

/* (d) Tokens that look like a difficulty request but carry no valid value are
 * reported; valid tokens and unrelated parameters are not. */
static void test_ignored_token_detector(void)
{
	static const char *const ignored[] = {
		"d=10M", "D=100000000", "d=1e8", "diff=", "d=0", "d=-5",
		"DIFF=64", "x,d=10M", "x;D=5", "x d=abc", "x\td=", "d= 1",
		"diff=512.5", "d=999999999999999999999999",
	};
	static const char *const accepted[] = {
		"x", "", "d=64", "diff=64", "worker_id=64", "pwd=64", "notdiff=64",
		"x,worker_id=64", "d=64,D=10M", "D=10M;diff=64", "id=5", "dd=5",
	};
	unsigned int i;

	for (i = 0; i < sizeof(ignored) / sizeof(ignored[0]); i++) {
		if (!password_diff_token_ignored(ignored[i])) {
			fprintf(stderr, "expected ignored: '%s'\n", ignored[i]);
			assert(0);
		}
	}
	for (i = 0; i < sizeof(accepted) / sizeof(accepted[0]); i++) {
		if (password_diff_token_ignored(accepted[i])) {
			fprintf(stderr, "expected not ignored: '%s'\n", accepted[i]);
			assert(0);
		}
	}
	/* The grammar for valid values is unchanged. */
	assert(password_requested_diff("d=64") == 64);
	assert(password_requested_diff("D=64") == 0);
	assert(password_requested_diff("d=128 diff=256") == 256);
}

int main(void)
{
	ckpool.mindiff = POOL_MINDIFF;
	ckpool.startdiff = 8;
	test_floor_client_ratchets();
	test_no_floor_client();
	test_target_clamps();
	test_pinned_client_holds();
	test_pinned_suggest_ignored();
	test_ignored_token_detector();
	test_reauth_unpins_plain();
	test_reauth_unpins_rental();
	test_reauth_unpins_highdiff();
	test_reauth_unpinned_untouched();
	test_repin_logs_suggest_again();
	puts("password_diff: all OK");
	return 0;
}
