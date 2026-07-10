# orun-agents-live — Risks & open questions

## Risks

| # | Risk | Mitigation |
|---|---|---|
| R1 | **Harness wire drift.** Claude Code's stream-JSON / control protocol evolves; a silent semantic change corrupts the event mapping. | Pin a minimum version + protocol handshake, refuse loud on mismatch (design §4.1); recorded fixtures make drift a failing test, not a field report; the conformance oracle keeps the seam honest. |
| R2 | **Delta volume.** Partial-text streaming per turn × multiple heads can dwarf event traffic (locally trivial, costly through the relay). | Deltas are cosmetic by contract (P§2): drop-first under backpressure, never stored, separate best-effort route in the cloud binding. Worst case degrades to turn-final rendering — the AG4 experience, not a failure. |
| R3 | **Multi-head races.** Two heads answer one approval; steer interleaving surprises. | Request-id binding + first-valid-verdict-wins (P§3), arrival order = log order, fixtures for both (P§7). Presence is advisory so there is no distributed-consensus surface to get wrong. |
| R4 | **The body as a local daemon-ish process.** `--detach` bodies outliving terminals invite orphans and confusion. | No daemon: bodies are per-session, self-terminating at seal, registry swept by pid-liveness on read (§3.2); `orun agent ps`/`kill` make the population visible and controllable. |
| R5 | **TUI frame corruption under chat load.** Streaming deltas + user typing + approval cards is the harshest render workload the cockpit has seen. | The existing frame-stability machinery is the contract; AL3 extends `live_scroll_test.go`-style invariant tests (every frame exactly terminal-sized) to the chat surface before polish. |
| R6 | **Interactive sessions blur the seal boundary.** A long-lived steered session ("interactive" runKind) has no natural terminal state. | `end` is explicit and cheap; idle timeout defaults to graceful `end` + seal; segments seal incrementally as today, so even a killed body loses at most the unsealed tail. |
| R7 | **Local socket security.** A same-machine attacker with the uid owns the session (and could approve tools). | Accepted: identical to `.orun/` and the model key already in env — the local trust model is the machine's. `0600`/`0700` enforced and tested; no network listener ever. |

## Open questions

| # | Question | Current lean |
|---|---|---|
| Q1 | **Local→cloud session migration** ("push this laptop session to a Daytona box and keep going")? | Out of v1. The brief is content-addressed so a *restart* in the cloud from the same brief is trivial; live migration of harness state is not, and Claude Code `--resume` semantics across hosts are unproven. Revisit post-AL4. |
| Q2 | **Default driver flip.** When does `--driver claude-code` become the default over `stub`? | When AL1 live smoke has run green for two weeks. Stub stays the test default forever. |
| Q3 | **Mid-turn vs turn-boundary steering.** Claude Code accepts mid-turn stdin; other harnesses may not. Do we promise mid-turn? | Promise turn-boundary; deliver mid-turn where the driver reports the capability (a driver metadata bit). The log's injected-order rule (P§3) holds either way. |
| Q4 | **Notifications, locally.** Terminal bell is weak for "approval waiting while you're in another mode/window." | Bell + tab badge in v1 (§5.1); OS notifications via a small hook (`orun agent run --notify-cmd`) as an AL5 nicety. Cloud notifications are AL8's, done properly. |
| Q5 | **Second driver.** Which harness proves "any binary" for the live plane (steer/interrupt support varies)? | Whatever passes conformance next matters less than *when*: after AL4, so the capability bits (Q3) are designed against two real data points, not one. |
| Q6 | **Presence identity locally.** Local heads all attribute to `usr_cli`; two terminals are indistinguishable in presence. | Fine for v1 (same human). Surface tag (`tui`/`cli`) is enough disambiguation; real multi-principal presence is inherently a cloud property. |
