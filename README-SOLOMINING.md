# BCH solo mining

Use [Installing CashStratum](docs/installation.md) for the supported installation flow,
Linux dependencies, command options and staged verification.

`scripts/install-cashstratum-solo.sh` builds a BCH Stratum V1 pool against an existing
Bitcoin Cash Node (BCHN). It does not install Bitcoin Core, alter node data or enable SV2.
Provide an explicit source checkout or public release tag, the service account, a BCH
payout address and RPC credentials. Run `bash scripts/install-cashstratum-solo.sh --help`
for the complete interface.

The pool must receive usable block templates from a synced node. In `-B` mode, miners
authenticate with their own BCH payout address, optionally followed by a worker suffix.
Accepted solves pay through the block coinbase, subject to the configured operator fee
and network coinbase maturity. See [pool fee mechanics](POOL_FEE.md), the
[configuration guide](README-CASHSTRATUM.md#-configuration) and [engine modes](README-CS_MODES.md).

Existing installations require a separate migration of configuration, logs, helper
socket names and historical solve protection. A staged install is not a production
cutover. Follow the [retention procedure](docs/operator-api.md#sharelog-retention) before
starting any cleanup timer on an existing log tree.
