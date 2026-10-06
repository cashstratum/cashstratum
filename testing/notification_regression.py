#!/usr/bin/env python3
"""Fault-inject the production ZMQ subscriber and notification RPC backstop."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
source = (ROOT/'src/stratifier.c').read_text()
start = source.index('static void *zmqnotify(')
end = source.index('\n#ifdef HAVE_CAPNP', start)
zmq = source[start:end]
start = source.index('static void *blockupdate(')
end = source.index('\n/* Enter holding workbase_lock', start)
block = source[start:end]
COMMON = r'''
#include <assert.h>
#include <errno.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <setjmp.h>
#define __maybe_unused
#define LOGERR(...) ((void)0)
#define LOGNOTICE(...) ((void)0)
#define LOGWARNING(...) ((void)0)
#define LOGDEBUG(...) ((void)0)
#define GEN_PRIORITY 1
#define fallthrough __attribute__((fallthrough))
#define GETBEST_NOTIFY 0
#define GETBEST_SUCCESS 1
#define GETBEST_FAILED -1
typedef struct { char lastswaphash[68]; } sdata_t;
static sdata_t state;
static struct { sdata_t *sdata; int btcdzmq_count, blockpoll; char **btcdzmq; char *zmqblock; } ckpool = { &state, 0, 100, NULL, "tcp://127.0.0.1:28332" };
static jmp_buf stop;
static int updates;
static void rename_proc(const char *s) {}
static int pthread_self(void) { return 0; }
static int pthread_detach(int x) { return 0; }
static void update_base(sdata_t *s, int priority) { updates++; }
'''
ZMQ = r'''
#define HAVE_ZMQ_H 1
#define ZMQ_SUB 1
#define ZMQ_SUBSCRIBE 2
#define ZMQ_POLLIN 1
#define ZMQ_DONTWAIT 1
typedef struct { void *socket; int fd; short events, revents; } zmq_pollitem_t;
typedef struct { int unused; } zmq_msg_t;
static int sockets, polls, connects, closes, receives;
static void *allocations[2];
static int allocated;
static void *ckzalloc(size_t n) { assert(allocated < 2); return allocations[allocated++] = calloc(1, n); }
static void *zmq_ctx_new(void) { return (void *)1; }
static void zmq_ctx_destroy(void *ctx) {}
static void *zmq_socket(void *ctx, int type) {
    sockets++; return sockets == 1 ? NULL : (void *)2;
}
static int zmq_setsockopt(void *s, int opt, const char *topic, size_t n) {
    assert(n == 9 && !memcmp(topic, "hashblock", 9)); return 0;
}
static int zmq_connect(void *s, const char *endpoint) { return ++connects == 1 ? -1 : 0; }
static int zmq_close(void *s) { assert(s); closes++; return 0; }
static void cksleep_ms(int n) { assert(n == 1000); }
static int zmq_poll(zmq_pollitem_t *items, int n, long timeout) {
    assert(timeout == 1000); assert(n == 1); polls++;
    if (polls == 7) longjmp(stop, 1);
    if (polls <= 2) { assert(!items[0].socket); return 0; }
    assert(items[0].socket);
    if (polls == 3) { errno = EINTR; return -1; }
    if (polls == 4) { errno = EINVAL; return -1; }
    items[0].revents = ZMQ_POLLIN; return 1;
}
static int zmq_msg_init(zmq_msg_t *m) { return 0; }
static int zmq_msg_recv(zmq_msg_t *m, void *s, int flags) {
    assert(flags == ZMQ_DONTWAIT);
    if (++receives == 1) { errno = EIO; return -1; } return 32;
}
static int zmq_msg_close(zmq_msg_t *m) { return 0; }
static int zmq_msg_more(zmq_msg_t *m) { return 0; }
static int zmq_msg_size(zmq_msg_t *m) { return 32; }
static void *zmq_msg_data(zmq_msg_t *m) { return NULL; }
static void __bin2hex(char *dst, void *data, int n) {}
'''
ZMQ_MAIN = r'''
int main(void) {
    if (!setjmp(stop)) zmqnotify(NULL);
    assert(sockets == 5); assert(closes == 3); assert(receives == 2); assert(updates == 1);
    for (int i = 0; i < allocated; i++) free(allocations[i]);
    puts("CashStratum ZMQ: socket/connect/poll/receive failure recovery and bounded waits passed");
    return 0;
}
'''
BLOCK = r'''
static int sleeps, polled;
static int generator_getbest(char *hash) { return GETBEST_NOTIFY; }
static int generator_pollbest(char *hash) {
    polled++; strcpy(hash, polled == 1 ? "old" : "new"); return GETBEST_SUCCESS;
}
static void cksleep_ms(int n) {
    assert(n == 5000);
    if (++sleeps == 3) longjmp(stop, 1);
}
'''
BLOCK_MAIN = r'''
int main(void) {
    strcpy(state.lastswaphash, "old");
    if (!setjmp(stop)) blockupdate(NULL);
    assert(polled == 2); assert(updates == 1);
    puts("CashStratum notifications: independent five-second tip polling catches missed blocks without redundant updates");
    return 0;
}
'''
with tempfile.TemporaryDirectory(prefix='cashstratum-notification-') as tmp:
    tmp = Path(tmp)
    for name, text in [('zmq', COMMON+ZMQ+zmq+ZMQ_MAIN), ('block', COMMON+BLOCK+block+BLOCK_MAIN)]:
        (tmp/(name+'.c')).write_text(text)
        subprocess.run([os.environ.get('CC', 'cc'), '-std=gnu11', '-g',
                        '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
                        str(tmp/(name+'.c')), '-o', str(tmp/name)], check=True)
        subprocess.run([str(tmp/name)], check=True)
