#!/usr/bin/env bash
# Register a self-hosted GitHub Actions runner for this repo.
#
# WHY THIS EXISTS: the PR gate's regtest money gate cannot run on a GitHub-hosted
# runner -- measured 2026-09-05, the e2e was killed at 28m20s by the 30-minute job
# timeout (run 33988688848) because CPU mining gets 2-4 cores there versus beast's
# 36, where the same run takes ~6 minutes. So the e2e job targets a self-hosted
# runner labelled [self-hosted, beast].
#
# RUNNERS ARE PER-REPOSITORY AND DO NOT FOLLOW A REPO MOVE OR RENAME. When this
# code moves to cashstratum/dev, re-run this against the new repo or the e2e job
# QUEUES FOREVER (timeout-minutes does not apply to queued time) and every PR
# shows a permanently pending check.
#
# Usage: RUNNER_TOKEN=$(gh api -X POST repos/<owner>/<repo>/actions/runners/registration-token --jq .token) \
#            bash scripts/install-gh-runner.sh
# Pass the token in the environment, not on this script's argv -- argv is visible
# in `ps` to every user on the box. Note this is not an end-to-end guarantee:
# GitHub's own config.sh has no env-var input mode, so the final `--token` flag
# below does put it on argv briefly. The ~1h single-use token lifetime is what
# bounds that window -- do not read this as a stronger guarantee than it is.
#
# The runner executes as an unprivileged `ghrunner` system user (nologin, no
# password, deliberately NOT in sudo or docker): workflow code runs with exactly
# that user's privileges. Safe here because the repo is private, so fork PRs
# cannot execute on it.
# Install a GitHub Actions self-hosted runner on beast for
# skaisser/blocksniper-ckpool, under its own unprivileged user.
#
# The token reaches this script via the environment (RUNNER_TOKEN) rather than
# its own command line. It still lands on config.sh's argv -- see the usage
# note above. It is short-lived (~1h) and single-use regardless.
set -euo pipefail

REPO_URL="https://github.com/skaisser/blocksniper-ckpool"
RUNNER_USER="ghrunner"
RUNNER_HOME="/opt/actions-runner"
RUNNER_VERSION="2.328.0"
LABELS="self-hosted,linux,x64,beast"

: "${RUNNER_TOKEN:?RUNNER_TOKEN must be set in the environment}"

if ! id "$RUNNER_USER" >/dev/null 2>&1; then
  # No login shell, no password, own home. Not in sudo/docker groups: workflow
  # code runs as this user, so any group it holds is a privilege the workflow
  # holds too.
  sudo useradd --system --create-home --home-dir "/home/$RUNNER_USER" \
       --shell /usr/sbin/nologin "$RUNNER_USER"
  echo "created user $RUNNER_USER"
else
  echo "user $RUNNER_USER already exists"
fi

sudo mkdir -p "$RUNNER_HOME"
sudo chown "$RUNNER_USER:$RUNNER_USER" "$RUNNER_HOME"

if [ ! -f "$RUNNER_HOME/config.sh" ]; then
  TARBALL="actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
  sudo -u "$RUNNER_USER" curl -fsSL --retry 3 \
    -o "$RUNNER_HOME/$TARBALL" \
    "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/${TARBALL}"
  sudo -u "$RUNNER_USER" tar -xzf "$RUNNER_HOME/$TARBALL" -C "$RUNNER_HOME"
  sudo -u "$RUNNER_USER" rm -f "$RUNNER_HOME/$TARBALL"
  echo "runner $RUNNER_VERSION unpacked"
else
  echo "runner already unpacked"
fi

sudo "$RUNNER_HOME/bin/installdependencies.sh" >/dev/null 2>&1 || true

if [ ! -f "$RUNNER_HOME/.runner" ]; then
  sudo -u "$RUNNER_USER" env RUNNER_TOKEN="$RUNNER_TOKEN" \
    "$RUNNER_HOME/config.sh" \
      --unattended --replace \
      --url "$REPO_URL" \
      --token "$RUNNER_TOKEN" \
      --name "beast" \
      --labels "$LABELS" \
      --work "_work"
  echo "runner configured"
else
  echo "runner already configured"
fi

# systemd service so it survives reboots.
cd "$RUNNER_HOME"
sudo ./svc.sh install "$RUNNER_USER"
sudo ./svc.sh start
sleep 3
sudo ./svc.sh status | head -20
