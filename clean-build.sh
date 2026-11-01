#!/bin/bash

# Clean all build artifacts from CashStratum source directory
# Use this if you accidentally ran 'make' instead of install script

set -euo pipefail

echo "Cleaning CashStratum build artifacts..."

# Remove binaries from root
rm -f cashstratum ckpool ckpmsg notifier test-driver 2>/dev/null

# Remove build artifacts
rm -f src/libcashstratum.a src/libckpool.a src/ckpool src/ckpmsg src/csproxy src/notifier 2>/dev/null
rm -f test/sha256 test/cashaddr test/addrclassify test/cbfinal test/merklebranch \
      test/nodecompat test/nodecompat_alloc test/nodecompat_yyjson test/reject_stats 2>/dev/null

# The vendored src/jansson-2.14 tree is gone: upstream ckpool 1.2.0 replaced
# jansson with yyjson, which is compiled straight into the daemon from
# src/yyjson.c. Nothing here to clean any more.

# Remove service files
rm -f cashstratum-regtest.service ckpool-regtest.service 2>/dev/null

# Run make clean if available
if [ -f Makefile ]; then
    echo "Running make clean..."
    make clean 2>/dev/null || true
fi

echo "✓ Build artifacts cleaned"
echo
echo "To build and install properly, use:"
echo "  ./install-cashstratum.sh   # For production"