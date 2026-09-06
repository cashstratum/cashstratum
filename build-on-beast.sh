#!/usr/bin/env bash
# Build this worktree on beast (Ubuntu 24.04, matches production). macOS lacks
# the autotools/glibc bits this Linux daemon needs, so never trust a local build.
# --disable-sv2 mirrors install-ckpool.sh: Stratum V2 is Bitcoin-only, and
# leaving it on auto makes the build flip depending on whether the host has
# libsodium and a populated src/secp256k1 submodule.
# Usage: ./build-on-beast.sh [--clean]
set -uo pipefail
REMOTE=beast
RDIR=/tmp/blocksniper-ckpool-rebase
HERE="$(cd "$(dirname "$0")" && pwd)"

rsync -az --delete \
  --exclude '.git' --exclude '.built-from' --exclude '*.o' --exclude '*.lo' --exclude '.deps' \
  --exclude 'autom4te.cache' --exclude 'src/ckpool' --exclude 'src/notifier' \
  "$HERE/" "$REMOTE:$RDIR/" || { echo "RSYNC FAILED"; exit 2; }

# The remote build dir is shared across branches. An incremental make over a
# tree rsynced from a DIFFERENT branch silently yields a binary that matches
# neither -- so stamp what we last built there and force a clean when it moves.
STAMP_LOCAL="$(git -C "$HERE" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
STAMP_REMOTE=$(ssh "$REMOTE" "cat $RDIR/.built-from 2>/dev/null" || true)
CLEAN=""
if [ "${1:-}" = "--clean" ]; then
  CLEAN="rm -rf autom4te.cache Makefile config.status; "
elif [ -n "$STAMP_REMOTE" ] && [ "$STAMP_REMOTE" != "$STAMP_LOCAL" ]; then
  echo "build dir last used by '$STAMP_REMOTE', now '$STAMP_LOCAL' -> forcing clean"
  CLEAN="rm -rf autom4te.cache Makefile config.status; "
fi
ssh "$REMOTE" "mkdir -p $RDIR && printf '%s' '$STAMP_LOCAL' > $RDIR/.built-from"
ssh "$REMOTE" "cd $RDIR && ${CLEAN}
  { [ -f Makefile ] || { ./autogen.sh && ./configure --disable-sv2; } ; } >/tmp/bs-conf.log 2>&1
  make -j\$(nproc) >/tmp/bs-build.log 2>&1
  RC=\$?
  echo \"make rc=\$RC\"
  if [ \$RC -ne 0 ]; then
    echo '--- first 40 error lines ---'
    grep -nE 'error:|Error |undefined reference' /tmp/bs-build.log | head -40
    grep -nE 'error:' /tmp/bs-conf.log | head -10
  else
    ls -l src/ckpool && echo BUILD_OK
  fi"
