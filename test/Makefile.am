AM_CPPFLAGS = -I$(top_srcdir)/src -I$(top_builddir)
LDADD = $(top_srcdir)/src/libckpool.a

check_PROGRAMS = sha256 cashaddr addrclassify cbfinal
TESTS = sha256 cashaddr addrclassify cbfinal

sha256_SOURCES = sha256.c

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

if HAVE_SV2
AM_CPPFLAGS += @SV2_CFLAGS@
check_PROGRAMS += sv2_codec sv2_codec_fuzz sv2_authority_b58 sv2_noise_hs sv2_hs_limit \
	sv2_idle sv2_tx sv2_cb sv2_bridge
TESTS += sv2_codec sv2_codec_fuzz sv2_authority_b58 sv2_noise_hs sv2_hs_limit sv2_idle \
	sv2_tx sv2_cb sv2_bridge
# Codec is in libckpool.a — do not list src/sv2_codec.c here (same object
# basename "sv2_codec.o" made test/.deps and src/.deps collide).
sv2_codec_SOURCES = sv2_codec.c
sv2_codec_LDADD = $(top_srcdir)/src/libckpool.a
sv2_codec_fuzz_SOURCES = sv2_codec_fuzz.c
sv2_codec_fuzz_LDADD = $(top_srcdir)/src/libckpool.a
# sv2_tx.c is in libckpool.a too (same object-basename collision rule).
sv2_tx_SOURCES = sv2_tx.c
sv2_tx_LDADD = $(top_srcdir)/src/libckpool.a
sv2_cb_SOURCES = sv2_cb.c
sv2_cb_LDADD = $(top_srcdir)/src/libckpool.a
# sv2_bridge.c is in libckpool.a too (same object-basename collision rule).
sv2_bridge_SOURCES = sv2_bridge.c
sv2_bridge_LDADD = $(top_srcdir)/src/libckpool.a
# Default mutational budget is enough for make check; override with SV2_FUZZ_ITERS
# noise/conn still need their own objects; codec comes from the library
sv2_authority_b58_SOURCES = sv2_authority_b58.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_authority_b58_LDADD = $(top_srcdir)/src/libckpool.a @SV2_LIBS@
sv2_authority_b58_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
sv2_noise_hs_SOURCES = sv2_noise_hs.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_noise_hs_LDADD = $(top_srcdir)/src/libckpool.a @SV2_LIBS@
sv2_noise_hs_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
sv2_hs_limit_SOURCES = sv2_hs_limit.c \
	$(top_srcdir)/src/sv2_conn.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_hs_limit_LDADD = $(top_srcdir)/src/libckpool.a @SV2_LIBS@
sv2_hs_limit_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
sv2_idle_SOURCES = sv2_idle.c \
	$(top_srcdir)/src/sv2_conn.c \
	$(top_srcdir)/src/sv2_noise.c
sv2_idle_LDADD = $(top_srcdir)/src/libckpool.a @SV2_LIBS@
sv2_idle_CPPFLAGS = $(AM_CPPFLAGS) -DHAVE_SV2
endif
