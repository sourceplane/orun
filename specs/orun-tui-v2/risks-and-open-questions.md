# Risks and open questions — orun-tui-v2

## Risks

**R1 — Rebuild scope creep.** A ground-up rebuild invites "while we're
here". Mitigation: the invariant list (design §13) is closed; the surface
map (design §5) is closed; anything not in them goes to the sharpness
register. TR3–TR7 are ports of *existing capability* onto the new kernel,
not feature work — the only genuinely new features are Work, Home/attention,
Events, and cloud connect (TR6–TR8).

**R2 — Two cockpits in the tree for months.** `internal/tui` (frozen) and
`internal/tui2` coexist through TR8. Risk: bug fixes needed in the old one
mid-epic. Policy: the old cockpit takes critical fixes only; everything else
is "fixed in v2". The freeze is what keeps the epic honest — if v1 keeps
absorbing improvements, v2 never wins the comparison.

**R3 — The memoization contract is subtle.** Region caching keyed on store
revisions fails silently if a renderer reads state it didn't declare.
Mitigation: renderers receive a *view* of the store that records reads in
debug builds and asserts they're covered by the declared revision keys; the
property tests render twice (cached vs cold) and diff.

**R4 — fs-watch reliability across platforms.** fsnotify on macOS
(FSEvents) and Linux (inotify) behave differently around rename-heavy
object-store writes; editors and `git checkout` cause storms. Mitigation:
debounce + ref-level (not object-level) watching; the ticker fallback is
kept, tested, and reported on the status line when active (`⏺ local ·
degraded refresh`).

**R5 — Step-level hooks change runner surface.** `BeforeStep`/`AfterStep`
touches `internal/runner`, which CI and cloud sandboxes also execute.
Mitigation: hooks are advisory/no-op by default, standalone PR, covered by
runner tests; no change to sealed records or the event vocabulary.

**R6 — Fold parity drifts.** The console's conversation fold evolves; Go
and TS folds disagree a month later. Mitigation: the parity goldens are the
*shared* fixtures in the contracts package — CI on both repos folds the same
files. New fixture = both heads update or one fails. (Same discipline that
kept attach v1 byte-identical.)

**R7 — Terminal matrix.** tmux, iTerm2, kitty, Windows Terminal, ssh with
high latency. The fixed-dim frame + no-ClearScreen design *reduces* exposure
(less output, no full repaints), but the TR9 pass needs an explicit matrix.
High-latency ssh is the case that most rewards region-diff output — measure
bytes/frame in the bench, not just time.

**R8 — Claude Code driver sessions are chatty.** Delta-heavy streams at
full tokens/sec must not starve the rest of the UI. Carried mitigations:
delta coalescing per frame tick (the log-batch pattern), bounded head queues
with `bye{lagged}`. The bench includes a "firehose session" replay.

## Open questions

**Q1 — Surface count: is Secrets worth a top-level slot?** It's thin
locally (chain refs + orphan warnings). Alternative: fold into Catalog
(entity-scoped) + Home warnings, freeing slot 7 for Docs. Leaning: keep
Secrets top-level for console parity and because orphaned brokered secrets
are an operator task; revisit at TR7 with usage.

**Q2 — Work lane depth offline.** Sealed epic/spec snapshots are read-only
frozen copies; the live work plane is cloud-only. Is a read-only local Work
surface confusing when offline? Options: (a) show snapshots with a clear
"sealed at <time>" banner, (b) hide the surface offline (violates
"connection is a status"). Leaning (a) — the banner *is* the honest state.

**Q3 — In-app sign-in vs deferring to `orun login`.** The device flow in
the TUI duplicates a small amount of `command_login.go`. Worth it for the
walk-up experience, but the refresh-lock interaction (`cliauth`
cross-process file lock) with a concurrently running CLI needs a test.

**Q4 — Remote execution timing.** Users who see remote runs will ask to
*dispatch* remotely. This epic keeps it out (design §14) because it needs
the coordination-loop UX (leases, claims) designed properly. Decide at TR8
whether the follow-on epic starts immediately.

**Q5 — Events surface value offline.** Local `ExecutionEvent`s are sparse.
If TR7 finds the local lane too thin, Events may become cloud-lane-only
content within the same surface (empty-state explains, palette still
deep-links) rather than being cut.

**Q6 — Prefix collision.** `TR` chosen for "TUI rebuild"; no existing
cluster uses it (WF/AL/AG/WP taken). Confirm no cloud-side cluster claims
`TR` before the first PR.

**Q7 — The reference video.** This design was authored from the code, the
profiling capture, and the console; the referenced walkthrough video was not
available. If it demonstrates bugs outside §1's classes (e.g. specific
terminal/locale artifacts), fold them into the TR0 property-test corpus.
