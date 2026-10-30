#!/bin/sh
#
# Stratum V2 Noise handshake interoperability test.
#
# Builds sv2-noise-probe (an initiator on the reference noise_sv2 crate) and
# points it at sv2_interop_responder, which serves the CashStratum side of the
# handshake on a loopback port:
#
#   leg 1  certificate version 0      -> the probe must accept it  (exit 0, "OK")
#   leg 2  certificate version 1 (-V) -> the probe must reject it  (exit 1,
#          InvalidCertificate naming "version: 1")
#
# Leg 2 is the negative control: it shows the probe really verifies the
# certificate instead of accepting whatever the responder sends.
#
# Environment:
#   SV2_INTEROP_RESPONDER  responder binary   (default ./sv2_interop_responder)
#   CARGO                  cargo executable   (default: cargo on PATH)
#   SV2_INTEROP_CRATE      probe crate dir    (default: this script's directory)
#   CARGO_TARGET_DIR       cargo build output (default $PWD/sv2-interop-target)
#
# Exit status: 0 both legs pass, 1 a leg failed, 77 (automake SKIP) when cargo
# is unavailable or the probe cannot be built (no toolchain, no network).

set -eu

SKIP=77

RESPONDER=${SV2_INTEROP_RESPONDER:-./sv2_interop_responder}
CRATE=${SV2_INTEROP_CRATE:-$(cd "$(dirname "$0")" && pwd)}
CARGO_TARGET_DIR=${CARGO_TARGET_DIR:-$PWD/sv2-interop-target}
export CARGO_TARGET_DIR

cargo_bin=${CARGO:-}
if [ -z "$cargo_bin" ]; then
	cargo_bin=cargo
fi
if ! cargo_path=$(command -v "$cargo_bin" 2>/dev/null) || [ -z "$cargo_path" ]; then
	echo "SKIP: cargo not found ('$cargo_bin'); install a Rust toolchain to run this test"
	exit $SKIP
fi

if [ ! -x "$RESPONDER" ]; then
	echo "FAIL: responder not found or not executable: $RESPONDER"
	exit 1
fi
if [ ! -f "$CRATE/Cargo.toml" ]; then
	echo "FAIL: probe crate not found: $CRATE/Cargo.toml"
	exit 1
fi

scratch=$(mktemp -d "${TMPDIR:-/tmp}/sv2_interop.XXXXXX")
harness_pid=

cleanup() {
	if [ -n "$harness_pid" ]; then
		kill "$harness_pid" 2>/dev/null || true
		wait "$harness_pid" 2>/dev/null || true
	fi
	rm -rf "$scratch"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

echo "cargo: $cargo_path"
echo "crate: $CRATE"
echo "responder: $RESPONDER"

if ! "$cargo_path" build --release --locked \
	--manifest-path "$CRATE/Cargo.toml" >"$scratch/cargo.log" 2>&1; then
	echo "SKIP: cargo build of the probe failed (toolchain or crates.io unavailable); last lines:"
	tail -n 20 "$scratch/cargo.log"
	exit $SKIP
fi

PROBE=$CARGO_TARGET_DIR/release/sv2-noise-probe
if [ ! -x "$PROBE" ]; then
	echo "FAIL: cargo build succeeded but $PROBE is missing"
	exit 1
fi

failures=0

# run_leg NAME EXPECT_RC [RESPONDER_ARGS...]
# Starts the responder for one connection, feeds its URL to the probe and
# checks the probe's exit status and output.
run_leg() {
	leg=$1
	expect_rc=$2
	shift 2

	url_file=$scratch/$leg.url
	err_file=$scratch/$leg.responder.err
	"$RESPONDER" -p 0 -n 1 "$@" >"$url_file" 2>"$err_file" &
	harness_pid=$!

	url=
	tries=0
	while [ $tries -lt 100 ]; do
		url=$(sed -n '1{/^stratum2+tcp:\/\/.*\/./p;}' "$url_file")
		if [ -n "$url" ]; then
			break
		fi
		if ! kill -0 "$harness_pid" 2>/dev/null; then
			break
		fi
		sleep 0.1
		tries=$((tries + 1))
	done
	if [ -z "$url" ]; then
		echo "$leg: FAIL: responder printed no URL; its stderr:"
		cat "$err_file"
		kill "$harness_pid" 2>/dev/null || true
		wait "$harness_pid" 2>/dev/null || true
		harness_pid=
		failures=$((failures + 1))
		return 0
	fi

	port=$(printf '%s\n' "$url" | sed -n 's|^stratum2+tcp://127\.0\.0\.1:\([0-9][0-9]*\)/.*$|\1|p')
	echo "$leg: responder args: -p 0 -n 1 $*"
	echo "$leg: url consumed: $url"

	probe_rc=0
	probe_out=$("$PROBE" 127.0.0.1 "$port" "$url" 2>"$scratch/$leg.probe.err") || probe_rc=$?
	echo "$leg: probe exit: $probe_rc"
	echo "$leg: probe output: $probe_out"
	if [ -s "$scratch/$leg.probe.err" ]; then
		echo "$leg: probe stderr:"
		cat "$scratch/$leg.probe.err"
	fi

	# The responder exits on its own after one connection; give it a moment,
	# then make sure it is gone.
	tries=0
	while [ $tries -lt 50 ] && kill -0 "$harness_pid" 2>/dev/null; do
		sleep 0.1
		tries=$((tries + 1))
	done
	kill "$harness_pid" 2>/dev/null || true
	harness_rc=0
	wait "$harness_pid" 2>/dev/null || harness_rc=$?
	harness_pid=
	echo "$leg: responder exit: $harness_rc"

	verdict=PASS
	if [ "$probe_rc" -ne "$expect_rc" ]; then
		verdict=FAIL
	elif [ "$expect_rc" -eq 0 ]; then
		[ "$probe_out" = "OK" ] || verdict=FAIL
	else
		case $probe_out in
		*InvalidCertificate*) ;;
		*) verdict=FAIL ;;
		esac
		case $probe_out in
		*"version: 1"*) ;;
		*) verdict=FAIL ;;
		esac
	fi
	if [ "$verdict" = FAIL ]; then
		echo "$leg: responder stderr:"
		cat "$err_file"
		failures=$((failures + 1))
	fi
	echo "$leg: $verdict (expected probe exit $expect_rc)"
	echo
}

run_leg "leg1-accept" 0
run_leg "leg2-reject-version" 1 -V 1

if [ "$failures" -ne 0 ]; then
	echo "sv2_interop: FAIL ($failures leg(s))"
	exit 1
fi
echo "sv2_interop: PASS"
exit 0
