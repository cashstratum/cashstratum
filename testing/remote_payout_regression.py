#!/usr/bin/env python3
"""Exercise production remote-user registration with injected script conversion results."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
source = (ROOT / 'src/stratifier.c').read_text()
start = source.index('static user_instance_t *generate_remote_user(')
end = source.index('\nstatic void parse_remote_share(', start)
HARNESS = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <alloca.h>
#define unlikely(x) (x)
#define LOGWARNING(...) ((void)0)
#define LOGNOTICE(...) ((void)0)
#define CANON_USERNAME_SIZE 256
#define strdupa(s) strcpy(alloca(strlen(s) + 1), s)
typedef struct { int unused; } sdata_t;
typedef struct { bool btcaddress, script; int txnlen; char txnbin[64]; } user_instance_t;
static struct { bool proxy; sdata_t *sdata; } ckpool;
static user_instance_t user;
static bool recognised, create_fails;
static int encoded_length, conversion_calls;
static void canonicalise_username(const char *src, char *dst, size_t size) {
    snprintf(dst, size, "%s", src);
}
static user_instance_t *get_create_user(sdata_t *data, const char *name, bool *is_new) {
    *is_new = true;
    return create_fails ? NULL : &user;
}
static bool generator_checkaddr(const char *name, bool *script, bool *segwit) {
    *script = false; *segwit = false; return recognised;
}
static int address_to_txn(char *out, const char *name, bool script, bool segwit) {
    conversion_calls++;
    if (encoded_length > 0) memset(out, 0x51, encoded_length);
    return encoded_length;
}
'''
CASES = r'''
int main(void) {
    recognised = true;
    for (int i = -1; i <= 0; i++) {
        memset(&user, 0, sizeof(user)); encoded_length = i;
        assert(generate_remote_user("bitcoincash:example.worker") == NULL);
        assert(!user.btcaddress && user.txnlen == 0);
    }
    for (int i = 23; i <= 25; i += 2) {
        memset(&user, 0, sizeof(user)); encoded_length = i;
        assert(generate_remote_user("bitcoincash:example.worker") == &user);
        assert(user.btcaddress && user.txnlen == i && user.txnbin[0] == 0x51);
    }
    memset(&user, 0, sizeof(user)); recognised = false;
    int previous = conversion_calls;
    assert(generate_remote_user("ordinary-worker") == &user);
    assert(!user.btcaddress && conversion_calls == previous);
    ckpool.proxy = true; recognised = true;
    assert(generate_remote_user("ordinary-worker") == &user);
    assert(!user.btcaddress && conversion_calls == previous);
    ckpool.proxy = false; create_fails = true;
    assert(generate_remote_user("ordinary-worker") == NULL);
    puts("CashStratum remote payout: failed encoding rejected, valid scripts and non-address mode preserved");
    return 0;
}
'''
with tempfile.TemporaryDirectory(prefix='cashstratum-remote-payout-') as tmp:
    tmp = Path(tmp)
    (tmp / 'remote.c').write_text(HARNESS + source[start:end] + CASES)
    subprocess.run([os.environ.get('CC', 'cc'), '-std=gnu11', '-g',
                    '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
                    str(tmp / 'remote.c'), '-o', str(tmp / 'remote')], check=True)
    subprocess.run([str(tmp / 'remote')], check=True)
