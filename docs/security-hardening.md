# CashStratum security hardening

This follow-up checks the historical findings in `SECURITY-share-inflation.md`
and an earlier comparison against other BCH pool forks against the current
source. These changes ship in CashStratum 1.2.1; this document does not certify
any running deployment.

## Findings and changes

| Area | Finding | Result |
| --- | --- | --- |
| Share acceptance | Historical pool-minimum acceptance inflated assigned-difficulty credit | The original assigned-difficulty check was already present and is preserved. |
| Difficulty requests | Suggestions and rental defaults could exceed `maxdiff`; password parsing accepted partial numbers and embedded `diff=` | Enforce configured bounds and complete numeric password tokens. |
| Share accounting | Rejected SV2 submissions and repeated stale SV1 submissions could affect rate estimates | Separate eligible rate credit from rejection and upstream forwarding. Preserve block detection and accepted latency-grace work. |
| Share logs | Writing by filename truncated the file despite an open append stream | Write through the append stream and check write/close failures. |
| Client framing | Parsing beyond a newline could accept malformed frames or dispatch a request twice | Parse exactly one complete frame, handle EOF, and preserve allocations on resize failure. |
| Node recovery | Earlier synchronous reconnect issue | The existing asynchronous reconnect fix is preserved. |
| Block notifications | Notification mode lacked an independent tip-polling fallback | Add a five-second RPC backstop, bounded ZMQ polling, and failed-socket recovery. |
| Large BCH templates | A 65,535-transaction cap silently discarded transactions | Use heap-backed merkle construction and 32-level branches across template and proxy paths. Reject malformed templates instead of substituting empty work. |
| Proxy branches | Existing length/count checks prevented overflow, but hexadecimal validation was incomplete | Validate branch encoding and use the common branch capacity. |
| Coinbase flags | Earlier unbounded auxiliary flags | Existing parser and builder limits are preserved. |
| Payout scripts | Startup and remote-user conversion results were unchecked | Refuse empty payout scripts before starting mining work or registering an address-based remote user. |
| Chain detection | RPC failure or an unknown chain silently selected mainnet | Retry without selecting a CashAddr network until a supported chain is identified. |

## Regression coverage

- Compiled production-function harnesses exercise difficulty parsing, stale and
  rejected-share accounting, append preservation, socket framing, network
  selection, and notification failure/recovery paths.
- The C merkle test compares reconstructed coinbase branches against independently
  calculated full trees, including 65,535, 65,536 and 300,000 transactions.
- The BCH regtest harness covers real block construction and payouts, share
  rejection, password requests, overrides and difficulty suggestions.

CashStratum remains the product name. Existing compatibility binary/library names
and CKPool authorship and licensing remain intact.
