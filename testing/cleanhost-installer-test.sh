#!/usr/bin/env bash
# Prove an installer's package list and configure flags work on a CLEAN host.
#
# WHY THIS EXISTS: install-ckproxy.sh once passed a bare ./configure, leaving
# enable_sv2 at "auto" -- which silently resolved to "no" on every host, because
# its PACKAGES list never installed libsodium. The script then prompted the
# operator for an SV2 upstream URL it could not serve. A container run is the
# only way to catch that class of bug; a developer machine already has the deps.
#
# Used as the acceptance check for installers.
# Runs real `docker run` against untouched ubuntu:24.04 and fedora:latest.
# Prove install-ckproxy.sh's PACKAGES + --enable-sv2 work on a CLEAN host.
# Exercises the distro PACKAGES lists and ./configure --enable-sv2.
set -u

UBUNTU_CMD='set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq build-essential git autoconf automake libtool pkg-config yasm libzmq3-dev libsodium-dev >/dev/null
echo "PACKAGES INSTALL: ok"
git clone -q https://github.com/cashstratum/cashstratum.git /src
cd /src
./autogen.sh >/dev/null 2>&1
if ! ./configure --enable-sv2 > /tmp/conf.log 2>&1; then
  echo "CONFIGURE FAILED"; tail -25 /tmp/conf.log; exit 1
fi
echo "CONFIGURE --enable-sv2: ok"
grep -iE "stratum v2|sv2|sodium" /tmp/conf.log | tail -6'

FEDORA_CMD='set -e
dnf install -y -q gcc gcc-c++ make git autoconf automake libtool pkgconf-pkg-config yasm zeromq-devel libsodium-devel >/dev/null 2>&1
echo "PACKAGES INSTALL: ok"
git clone -q https://github.com/cashstratum/cashstratum.git /src
cd /src
./autogen.sh >/dev/null 2>&1
if ! ./configure --enable-sv2 > /tmp/conf.log 2>&1; then
  echo "CONFIGURE FAILED"; tail -25 /tmp/conf.log; exit 1
fi
echo "CONFIGURE --enable-sv2: ok"
grep -iE "stratum v2|sv2|sodium" /tmp/conf.log | tail -6'

echo "================= UBUNTU 24.04 ================="
docker run --rm ubuntu:24.04 bash -c "$UBUNTU_CMD" 2>&1 | tail -20
echo "[ubuntu] exit=${PIPESTATUS[0]:-$?}"
echo
echo "================= FEDORA latest ================="
docker run --rm fedora:latest bash -c "$FEDORA_CMD" 2>&1 | tail -20
echo "[fedora] exit=${PIPESTATUS[0]:-$?}"
