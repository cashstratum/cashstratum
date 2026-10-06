#!/usr/bin/env python3
"""Compile production difficulty functions and exercise their accounting contracts."""
import os
import re
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


def function(source, name):
    """Return a complete C function, stopping before its next top-level declaration."""
    start = re.search(r'^(?:static )?(?:void|bool|int64_t) ' + name + r'\(', source, re.M).start()
    end = source.index('\n}', start) + 2
    return source[start:end]


class ShareSecurityRegression(unittest.TestCase):
    def test_sharelog_appends_instead_of_truncating(self):
        source = (ROOT / 'src/stratifier.c').read_text()
        start = source.index('\tif (ckpool.logshares)', source.index('static bool parse_submit('))
        end = source.index('\n\tif (ckpool.remote)', start)
        fragment = source[start:end]
        code = r'''
#include <stdio.h>
#include "yyjson.h"
#define unlikely(x) (x)
#define likely(x) (x)
#define LOGERR(...) ((void)0)
static struct { int logshares; } ckpool = {1};
int main(int argc, char **argv) {
    (void)argc;
    const char *fname=argv[1]; FILE *fp;
    for (int i=0;i<2;i++) {
        yyjson_mut_doc *doc=yyjson_mut_doc_new(NULL);
        yyjson_mut_val *root=yyjson_mut_obj(doc);
        yyjson_mut_doc_set_root(doc, root);
        yyjson_mut_obj_add_int(doc, root, "share", i);
''' + fragment + r'''
        yyjson_mut_doc_free(doc);
    }
    return 0;
}
'''
        with tempfile.TemporaryDirectory(prefix='cashstratum-sharelog-') as tmp:
            path = Path(tmp)
            (path / 'test.c').write_text(code)
            subprocess.run([os.environ.get('CC', 'cc'), '-std=c11', '-Wall', '-Werror',
                            '-I', str(ROOT / 'src'), str(path / 'test.c'),
                            str(ROOT / 'src/yyjson.c'), '-o', str(path / 'test')], check=True)
            subprocess.run([str(path / 'test'), str(path / 'shares')], check=True)
            self.assertEqual((path / 'shares').read_text().splitlines(),
                             ['{"share":0}', '{"share":1}'])

    def test_production_difficulty_and_accounting(self):
        source = (ROOT / 'src/stratifier.c').read_text()
        code = r'''
#include <assert.h>
#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#define MIN(a,b) ((a)<(b)?(a):(b))
#define unlikely(x) (x)
#define LOGINFO(...) ((void)0)
#define LOGNOTICE(...) ((void)0)
typedef struct { double best_diff; char *workername; } worker_instance_t;
typedef struct { char *username; } user_instance_t;
typedef struct { int64_t id; double diff, network_diff; } workbase_t;
typedef struct { int workbase_lock; int64_t workbase_id; workbase_t *current_workbase; } sdata_t;
typedef struct {
    bool authorised; double best_diff; int64_t diff, old_diff, suggest_diff, diff_change_job_id;
    char *identity; worker_instance_t *worker_instance; user_instance_t *user_instance; sdata_t *sdata;
} stratum_instance_t;
static struct { int64_t mindiff, maxdiff; sdata_t *sdata; } ckpool;
static stratum_instance_t client;
static bool unique_share, last_valid, last_submit;
static double credited;
static int diff_sent;
static void ck_rlock(int *l) { (void)l; }
static void ck_runlock(int *l) { (void)l; }
static stratum_instance_t *ref_instance_by_id(sdata_t *s, int64_t id) { (void)s; (void)id; return &client; }
static void dec_instance_ref(sdata_t *s, stratum_instance_t *c) { (void)s; (void)c; }
static void check_best_diff(sdata_t *s, user_instance_t *u, worker_instance_t *w, double d, stratum_instance_t *c) { (void)s;(void)u;(void)w;(void)d;(void)c; }
static bool new_share(sdata_t *s, const unsigned char *h, int64_t id) { (void)s;(void)h;(void)id; return unique_share; }
static void add_submit(stratum_instance_t *c, double d, bool valid, bool submit) { (void)c;last_valid=valid;last_submit=submit;if(valid||submit)credited+=d; }
static bool client_active(stratum_instance_t *c) { return c->authorised; }
static void stratum_send_diff(sdata_t *s, stratum_instance_t *c) { (void)s;(void)c;diff_sent++; }
typedef struct { double value; } yyjson_mut_val;
static yyjson_mut_val *yyjson_mut_arr_get(yyjson_mut_val *v, int i) { (void)i; return v; }
static bool yyjson_mut_is_num(yyjson_mut_val *v) { return v != NULL; }
static double yyjson_mut_get_num(yyjson_mut_val *v) { return v->value; }
'''
        for name in ('clamp_requested_diff', 'password_requested_diff',
                     'suggest_diff', 'stratifier_sv2_account_share'):
            code += '\n' + function(source, name) + '\n'
        start = source.index('\trate_submit = submit;', source.index('static bool parse_submit('))
        end = source.index('\n\tadd_submit', start)
        code += "\nstatic bool stale_accounting(sdata_t *sdata, const unsigned char *hash, int64_t id, double sdiff, double diff, bool stale, bool submit, bool result) {\nbool rate_submit;\n" + source[start:end] + "\nreturn rate_submit;\n}\n"
        code += r'''
int main(void) {
    workbase_t wb = {.id=100, .diff=1, .network_diff=1000000};
    sdata_t data = {.workbase_id=100, .current_workbase=&wb};
    worker_instance_t worker = {0}; user_instance_t user = {0};
    unsigned char hash[32] = {0}; char err[80]; bool network;
    ckpool.sdata=&data; ckpool.mindiff=64; ckpool.maxdiff=4096;
    client=(stratum_instance_t){.authorised=true,.sdata=&data,.worker_instance=&worker,.user_instance=&user,.diff=64};
    assert(password_requested_diff("d=128") == 128);
    assert(password_requested_diff("x;diff=256,foo=1") == 256);
    assert(password_requested_diff("worker_id=512") == 0);
    assert(password_requested_diff("notdiff=512") == 0);
    assert(password_requested_diff("d=512garbage") == 0);
    assert(password_requested_diff("diff=512.5") == 0);
    assert(password_requested_diff("d=999999999999999999999999") == 0);
    assert(password_requested_diff("d=-5") == 0);
    assert(password_requested_diff("d= 512") == 0);
    assert(password_requested_diff("d=128 diff=256") == 256);
    yyjson_mut_val request={.value=1000000000};
    suggest_diff(&client,"mining.suggest_difficulty",&request);
    assert(client.diff==4096 && client.suggest_diff==4096 && diff_sent==1);
    suggest_diff(&client,"mining.suggest_difficulty(999999)",NULL);
    assert(client.diff==4096 && diff_sent==1);
    request.value=1; suggest_diff(&client,"mining.suggest_difficulty",&request);
    assert(client.diff==64 && client.suggest_diff==64);
    request.value=INFINITY; suggest_diff(&client,"mining.suggest_difficulty",&request);
    assert(client.diff==64);
    ckpool.maxdiff=0; assert(clamp_requested_diff(1000000)==1000000);
    /* Even meeting upstream/network work cannot make a duplicate count. */
    unique_share=false;
    assert(stale_accounting(&data,hash,100,64,64,true,true,true));
    assert(!stale_accounting(&data,hash,100,1000000,64,true,true,false));
    assert(!stratifier_sv2_account_share(1,100,hash,1000000,err,sizeof(err),&network));
    assert(!strcmp(err,"duplicate-share") && !last_valid && !last_submit && credited==0 && !network);
    /* Meeting workbase difficulty but missing the client's target cannot count. */
    unique_share=true;
    assert(!stale_accounting(&data,hash,100,1,64,true,true,false));
    assert(stale_accounting(&data,hash,100,64,64,true,true,false));
    assert(!stratifier_sv2_account_share(1,100,hash,1,err,sizeof(err),&network));
    assert(!strcmp(err,"difficulty-too-low") && !last_submit && credited==0);
    assert(stratifier_sv2_account_share(1,100,hash,64,err,sizeof(err),&network));
    assert(last_valid && credited==64);
    /* Retarget grace must credit the lower difficulty used for validation. */
    client.diff=128;client.old_diff=64;client.diff_change_job_id=101;
    assert(stratifier_sv2_account_share(1,100,hash,64,err,sizeof(err),&network));
    assert(credited==128);
    return 0;
}
'''
        with tempfile.TemporaryDirectory(prefix='cashstratum-shares-') as tmp:
            path = Path(tmp)
            (path / 'test.c').write_text(code)
            subprocess.run([os.environ.get('CC', 'cc'), '-std=c11', '-Wall', '-Wextra',
                            '-Werror', '-fsanitize=undefined', str(path / 'test.c'),
                            '-o', str(path / 'test')], check=True)
            subprocess.run([str(path / 'test')], check=True)


if __name__ == '__main__':
    unittest.main()
