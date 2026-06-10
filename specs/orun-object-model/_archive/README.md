# `_archive/` — Implementation-time records (NOT the authoritative spec)

These documents are **frozen historical records** of *how and when* the object
model was built. The roadmap is complete (M0–M13 shipped; the object model is the
unconditional, single persistence stack), so this material is preserved for
provenance only — it is **not** the reference for how orun works today.

For current behaviour and architecture, read the authoritative spec one directory
above: `README.md`, `why-this-model.md`, `design.md`, `object-store.md`,
`identity-and-keys.md`, `data-model.md`, `runner-integration.md`,
`remote-and-consumers.md`, `cli-surface.md`, `compatibility-and-migration.md`,
`risks-and-open-questions.md`, and the forward-looking `FOLLOW-UPS.md`.

> These files may reference sibling specs by their original bare filenames (e.g.
> `object-store.md`, `design.md`) — those now live **one directory up**. Pre-move
> paths are kept as-is, per the archive convention.

## Contents

| Doc | What it was |
|-----|-------------|
| `IMPLEMENTATION-STATUS.md` | The as-built record — M0–M13 milestones, the M12 cutover steps, layer/coverage table, and test results at completion. |
| `implementation-plan.md` | The milestone plan: M0 → M13, each with goal, dependencies, suggested PR scope, and "done when" criteria. |
| `M12-native-runner-rewrite.md` | M12 sub-plan: the staged native-runner rewrite + legacy cutover. |
| `M12-hydrate-refactor.md` | M12 sub-plan: sealing `orun github pull` runs into the object graph. |
| `M12-tui-repoint.md` | M12 sub-plan: repointing the TUI/cockpit onto the object graph. |
| `test-plan.md` | Coverage targets, property tests, and the E2E walk used to verify the build. |
| `claude-goals.md` | Operating goals, constraints, and definition-of-done for the implementing agents. |
