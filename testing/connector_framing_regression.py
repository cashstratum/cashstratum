#!/usr/bin/env python3
"""Run the actual SV1 connector parser over socketpairs with the bundled yyjson.

The narrow harness replaces downstream queues and locks, not parsing or I/O,
so it runs without a BCH node, Linux epoll, or the full pool build.
"""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
source = (ROOT / 'src/connector.c').read_text()
start = source.index('static bool parse_client_msg(cdata_t *cdata, client_instance_t *client)')
end = source.index('\nstatic client_instance_t *ref_client_by_id', start)
parser = source[start:end]
HARNESS = r'''
#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>
#include "yyjson.h"
#define MAX_MSGSIZE 1024
#define MAX_REMOTE_MSGSIZE (16 * 1024 * 1024)
#define likely(x) (x)
#define unlikely(x) (x)
#define LOGNOTICE(...) ((void)0)
#define LOGERR(...) ((void)0)
#define LOGINFO(...) ((void)0)
#define ckyyalc (*((yyjson_alc *)NULL))
#define ck_wlock(x) ((void)0)
#define ck_wunlock(x) ((void)0)
#define safecmp strcmp
struct { bool redirector, passthrough, node; } ckpool;
typedef struct { int lock; } cdata_t;
typedef struct {
    char *buf; unsigned long bufofs; int fd, server;
    int64_t id; char address_name[64];
    bool remote, passthrough, got_msg, invalid;
} client_instance_t;
static int delivered;
static size_t round_up_page(size_t n) { return (n + 4095) & ~(size_t)4095; }
static void send_client(cdata_t *c, int64_t id, char *buf) { free(buf); }
static void strip_reserved_keys(yyjson_mut_val *r, bool address) {}
static void parse_redirector_share(cdata_t *c, client_instance_t *i, yyjson_mut_val *r) {}
static void stratifier_add_yyrecv(yyjson_mut_doc *d) { delivered++; yyjson_mut_doc_free(d); }
static void generator_add_send(yyjson_mut_doc *d) { stratifier_add_yyrecv(d); }
'''
CASES = r'''
static void run_case(const char *data, size_t len, bool accepted, int messages) {
    int fd[2]; cdata_t ctx = {0}; client_instance_t client = {0};
    assert(socketpair(AF_UNIX, SOCK_STREAM, 0, fd) == 0);
    assert(fcntl(fd[0], F_SETFL, O_NONBLOCK) == 0);
    client.fd = fd[0]; client.buf = calloc(1, 4096);
    assert(client.buf); delivered = 0;
    assert(write(fd[1], data, len) == (ssize_t)len);
    assert(parse_client_msg(&ctx, &client) == accepted);
    assert(delivered == messages);
    close(fd[0]); close(fd[1]); free(client.buf);
}
#define CASE(s, ok, n) run_case(s, sizeof(s) - 1, ok, n)
int main(void) {
    CASE("{\"id\":1}\n", true, 1);
    CASE("{\"id\":1}\r\n{\"id\":2}\n", true, 2);
    CASE("{\"id\":1}  \n", true, 1);
    CASE("{\"id\":1}garbage\n", false, 0);
    CASE("{\"id\":1}{}\n", false, 0);
    CASE("{\"id\":1}\0hidden\n", false, 0);
    CASE("{\"id\":\n{} }\n", false, 0);
    CASE("[]\n", false, 0);
    CASE("{broken}\n", false, 0);
    int fd[2]; cdata_t ctx = {0}; client_instance_t client = {0};
    assert(socketpair(AF_UNIX, SOCK_STREAM, 0, fd) == 0);
    assert(fcntl(fd[0], F_SETFL, O_NONBLOCK) == 0);
    client.fd = fd[0]; client.buf = calloc(1, 4096); delivered = 0;
    assert(write(fd[1], "{\"id\":", 6) == 6);
    assert(parse_client_msg(&ctx, &client)); assert(delivered == 0);
    assert(write(fd[1], "1}\n", 3) == 3);
    assert(parse_client_msg(&ctx, &client)); assert(delivered == 1);
    close(fd[1]);
    /* EOF must not retain an already-active client indefinitely. */
    assert(!parse_client_msg(&ctx, &client));
    close(fd[0]); free(client.buf);
    puts("CashStratum connector: framing, fragmentation, coalescing and EOF passed");
    return 0;
}
'''
with tempfile.TemporaryDirectory(prefix='cashstratum-framing-') as tmp:
    tmp = Path(tmp)
    (tmp / 'parser.c').write_text(HARNESS + parser + CASES)
    subprocess.run([os.environ.get('CC', 'cc'), '-std=gnu11', '-g',
                    '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
                    '-I'+str(ROOT/'src'), str(tmp/'parser.c'),
                    str(ROOT/'src/yyjson.c'), '-o', str(tmp/'parser')], check=True)
    subprocess.run([str(tmp/'parser')], check=True)
