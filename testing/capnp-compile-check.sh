#!/usr/bin/env bash
# Compile the #ifdef HAVE_CAPNP region of src/stratifier.c.
#
# WHY THIS EXISTS: no build path in this repo has libcapnp-dev -- release-gate.yml,
# pr-gate.yml and build-on-beast.sh all run without it -- so configure sets
# CAPNP=no and the preprocessor discards build_ipc_workbase() entirely, syntax
# errors included. An investigation once proposed a substantial patch to that
# exact function; nothing in CI would have caught even a typo in it.
#
# Builds in a container that HAS the dep, so the region is actually parsed.
# Compile the #ifdef HAVE_CAPNP region of src/stratifier.c, which no build path
# in this repo has ever parsed: release-gate.yml and build-on-beast.sh both run
# on hosts without libcapnp-dev, so configure sets CAPNP=no and the preprocessor
# discards build_ipc_workbase() entirely -- syntax errors included.
set -u
SRC=/tmp/capnp-check-src
rm -rf "$SRC"; cp -a /tmp/blocksniper-ckpool-rebase "$SRC"
rm -rf "$SRC"/{.git,autom4te.cache}; find "$SRC" -name '*.o' -delete 2>/dev/null

CMD='set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq build-essential autoconf automake libtool pkg-config \
  libssl-dev libzmq3-dev yasm libcapnp-dev capnproto >/dev/null
echo "DEPS: ok  (capnp $(capnp --version 2>&1 | head -1))"
pkg-config --exists capnp-rpc && echo "pkg-config capnp-rpc: FOUND" || { echo "capnp-rpc NOT found"; exit 1; }
cd /src
./autogen.sh >/dev/null 2>&1
./configure --disable-sv2 > /tmp/conf.log 2>&1 || { echo "CONFIGURE FAILED"; tail -20 /tmp/conf.log; exit 1; }
echo "--- configure summary (capnp line) ---"
grep -iE "capn|mining ipc" /tmp/conf.log | tail -5
grep -q "define HAVE_CAPNP 1" config.h && echo "HAVE_CAPNP: DEFINED (the region will compile)" || { echo "HAVE_CAPNP NOT defined -- test is meaningless"; exit 1; }
echo "--- building ---"
make -j"$(nproc)" > /tmp/build.log 2>&1
RC=$?
echo "make rc=$RC"
if [ $RC -ne 0 ]; then
  echo "--- errors ---"
  grep -nE "error:|Error |undefined reference" /tmp/build.log | head -30
  exit 1
fi
echo "BUILD OK with HAVE_CAPNP"
grep -cE "warning:" /tmp/build.log | sed "s/^/total warnings: /"
grep -nE "stratifier\.c.*(warning|error)" /tmp/build.log | head -15 || echo "(no stratifier.c diagnostics)"'

docker run --rm -v "$SRC":/src ubuntu:24.04 bash -c "$CMD" 2>&1 | tail -40
echo "[capnp-check] exit=${PIPESTATUS[0]:-$?}"
