AM_CPPFLAGS = -I$(top_srcdir)/src -I$(top_builddir)
LDADD = $(top_builddir)/src/libckpool.a

check_PROGRAMS = sha256 cashaddr addrclassify cbfinal merklebranch nodecompat reject_stats
TESTS = sha256 cashaddr addrclassify cbfinal merklebranch nodecompat reject_stats

sha256_SOURCES = sha256.c
merklebranch_SOURCES = merklebranch.c

# Rejected-difficulty accounting: the per-connection cap and saturating
# counters. reject_stats.c includes src/stratifier.c to exercise its actual
# accounting path; unrelated server entry points and their dependencies are
# discarded when linking.
reject_stats_SOURCES = reject_stats.c
reject_stats_CPPFLAGS = $(AM_CPPFLAGS) -I$(top_srcdir)/ipc/mining
reject_stats_CFLAGS = $(AM_CFLAGS) $(CFLAGS) -ffunction-sections -fdata-sections
reject_stats_LDFLAGS = -Wl,--gc-sections

# BCH address unit tests (fork-local): CashAddr decode/checksum vectors and
# the local cashaddr+legacy classifier that feeds address_to_txn().
cashaddr_SOURCES = cashaddr.c
addrclassify_SOURCES = addrclassify.c

# Coinbase finality predicate (fork-local): the four nSequence/nLockTime
# combinations from the finality specification, plus malformed-buffer handling. The predicate
# itself comes from libckpool.a -- do not list src/coinbase_final.c here, or
# test/.deps and src/.deps collide on the same "coinbase_final.o" basename
# (same rule as sv2_codec below), which is why the program is named cbfinal.
cbfinal_SOURCES = cbfinal.c

# Node RPC compatibility (BCHN, BCHD, Flowee the Hub): request ids, the GBT
# capability list and template parsing. nodecompat.c includes src/bitcoin.c
# itself so the RPC layer can be stubbed; see the file header.
nodecompat_SOURCES = nodecompat.c nodecompat_yyjson.c nodecompat_alloc.c

if HAVE_SV2
AM_CPPFLAGS += @SV2_CFLAGS@
check_PROGRAMS += sv2_codec sv2_codec_fuzz sv2_authority_b58 sv2_noise_hs sv2_hs_limit \
	sv2_idle sv2_tx sv2_cb sv2_bridge
TESTS += sv2_codec sv2_codec_fuzz sv2_authority_b58 sv2_noise_hs sv2_hs_limit sv2_idle \
	sv2_tx sv2_cb sv2_bridge
# Codec is in libckpool.a — do not list src/sv2_codec.c here (same object
# basename "sv2_codec.o" made test/.deps and src/.deps collide).
sv2_codec_SOURCES = sv2_codec.c
sv2_codec_LDADD = $(top_builddir)/src/libckpool.a
sv2_codec_fuzz_SOURCES = sv2_codec_fuzz.c
sv2_codec_fuzz_LDADD = $(top_builddir)/src/libckpool.a
# sv2_tx.c is in libckpool.a too (same object-basename collision rule).
sv2_tx_SOURCES = sv2_tx.c
sv2_tx_LDADD = $(top_builddir)/src/libckpool.a
sv2_cb_SOURCES = sv2_cb.c
sv2_cb_LDADD = $(top_builddir)/src/libckpool.a
# sv2_bridge.c is in libckpool.a too (same object-basename collision rule).
sv2_bridge_SOURCES = sv2_bridge.c
sv2_bridge_LDADD = $(top_builddir)/src/libckpool.a
# Default mutational budget is enough for make check; override with SV2_FUZZ_ITERS
# noise/conn still need their own objects; codec comes from the library
sv2_authority_b58_SOURCES = sv2_authority_b58.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_authority_b58_LDADD = $(top_builddir)/src/libckpool.a @SV2_LIBS@
sv2_authority_b58_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
sv2_noise_hs_SOURCES = sv2_noise_hs.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_noise_hs_LDADD = $(top_builddir)/src/libckpool.a @SV2_LIBS@
sv2_noise_hs_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
sv2_hs_limit_SOURCES = sv2_hs_limit.c \
	$(top_srcdir)/src/sv2_conn.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_hs_limit_LDADD = $(top_builddir)/src/libckpool.a @SV2_LIBS@
sv2_hs_limit_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
sv2_idle_SOURCES = sv2_idle.c \
	$(top_srcdir)/src/sv2_conn.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_idle_LDADD = $(top_builddir)/src/libckpool.a @SV2_LIBS@
sv2_idle_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
# Loopback SV2 responder driven by the interop test below. It is a server, so
# it is built for make check but never listed in TESTS.
check_PROGRAMS += sv2_interop_responder
sv2_interop_responder_SOURCES = sv2_interop_responder.c \
	$(top_srcdir)/src/sv2_conn.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_interop_responder_LDADD = $(top_builddir)/src/libckpool.a @SV2_LIBS@
sv2_interop_responder_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
endif

# Stratum V2 interoperability test (./configure --enable-sv2-interop). The
# wrapper is copied into the build directory under the name sv2_interop, so
# its results land in test/sv2_interop.log and test/sv2_interop.trs rather
# than next to the source. It exits 77 (SKIP) when cargo is unavailable or
# the crate cannot be built.
if HAVE_SV2_INTEROP
check_SCRIPTS = sv2_interop
TESTS += sv2_interop
AM_TESTS_ENVIRONMENT = \
	SV2_INTEROP_RESPONDER=./sv2_interop_responder; export SV2_INTEROP_RESPONDER; \
	CARGO='$(CARGO)'; export CARGO; \
	SV2_INTEROP_CRATE='$(abs_top_srcdir)/testing/sv2-interop'; export SV2_INTEROP_CRATE; \
	CARGO_TARGET_DIR='$(abs_builddir)/sv2-interop-target'; export CARGO_TARGET_DIR;
endif

sv2_interop: $(top_srcdir)/testing/sv2-interop/sv2_interop.sh
	$(AM_V_GEN)cp $(top_srcdir)/testing/sv2-interop/sv2_interop.sh $@ && chmod +x $@

CLEANFILES = sv2_interop

# Distributed in every configuration so that make distcheck finds them.
EXTRA_DIST = $(top_srcdir)/testing/sv2-interop/sv2_interop.sh \
	$(top_srcdir)/testing/sv2-interop/Cargo.toml \
	$(top_srcdir)/testing/sv2-interop/Cargo.lock \
	$(top_srcdir)/testing/sv2-interop/src/main.rs

clean-local:
	rm -rf sv2-interop-target
