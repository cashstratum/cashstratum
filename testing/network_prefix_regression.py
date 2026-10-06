#!/usr/bin/env python3
"""Exercise production network detection with real yyjson and controlled RPC replies."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
source = (ROOT/'src/generator.c').read_text()
start = source.index('static bool detect_cashaddr_prefix(')
end = source.index('\n/* Use a temporary fd', start)
HARNESS = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include "yyjson.h"
#define LOGWARNING(...) ((void)0)
#define LOGNOTICE(...) ((void)0)
#define dealloc(x) do { free(x); x = NULL; } while (0)
typedef struct { int unused; } connsock_t;
struct { char *cashaddr_prefix; } ckpool;
static const char *response;
static int applied;
static yyjson_doc *yyjson_rpc_call(connsock_t *cs, const char *request) {
    return response ? yyjson_read(response, strlen(response), 0) : NULL;
}
static void bch_set_cashaddr_prefix(const char *prefix) { applied++; }
'''
CASES = r'''
int main(void) {
    connsock_t cs = {0};
    const char *bad[] = {NULL, "{}", "{\"error\":{\"code\":-28}}",
        "{\"result\":{}}", "{\"result\":{\"chain\":null}}",
        "{\"result\":{\"chain\":3}}", "{\"result\":{\"chain\":\"unknown\"}}"};
    for (size_t i = 0; i < sizeof(bad)/sizeof(*bad); i++) {
        response = bad[i]; assert(!detect_cashaddr_prefix(&cs));
        assert(!ckpool.cashaddr_prefix); assert(applied == 0);
    }
    const char *chains[] = {"main", "test", "test4", "scale", "chip", "testnet4", "scalenet", "chipnet", "regtest"};
    for (size_t i = 0; i < sizeof(chains)/sizeof(*chains); i++) {
        char buf[128]; snprintf(buf, sizeof(buf), "{\"result\":{\"chain\":\"%s\"}}", chains[i]);
        response = buf; assert(detect_cashaddr_prefix(&cs));
        assert(!strcmp(ckpool.cashaddr_prefix, i == 0 ? "bitcoincash" : i == 8 ? "bchreg" : "bchtest"));
        dealloc(ckpool.cashaddr_prefix);
    }
    puts("CashStratum network prefix: RPC failure, malformed/unknown chain, retry and all supported networks passed");
    return 0;
}
'''
# Verify the caller cannot activate a server when detection failed.
assert 'if (unlikely(!ckpool.cashaddr_prefix) && !detect_cashaddr_prefix(cs))\n\t\tgoto out;' in source
with tempfile.TemporaryDirectory(prefix='cashstratum-network-') as tmp:
    tmp = Path(tmp)
    (tmp/'network.c').write_text(HARNESS + source[start:end] + CASES)
    subprocess.run([os.environ.get('CC', 'cc'), '-std=gnu11', '-g',
                    '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
                    '-I'+str(ROOT/'src'), str(tmp/'network.c'),
                    str(ROOT/'src/yyjson.c'), '-o', str(tmp/'network')], check=True)
    subprocess.run([str(tmp/'network')], check=True)
