# orun-docs-discipline — Implementation status

**Complete.** All six milestones are merged and released (v2.73.0–v2.73.5).

| Milestone | Status | PR | Release |
|---|---|---|---|
| PD1 — The narrative: tagline, landing page, information architecture | Done | [#710](https://github.com/sourceplane/orun/pull/710) | v2.73.0 |
| PD2 — Pillar 1, Declare | Done | [#711](https://github.com/sourceplane/orun/pull/711) | v2.73.1 |
| PD3 — Pillar 2, Package & evolve | Done | [#712](https://github.com/sourceplane/orun/pull/712) | v2.73.2 |
| PD4 — Pillar 3, Ground agents | Done | [#714](https://github.com/sourceplane/orun/pull/714) | v2.73.3 |
| PD5 — Pillar 4, Verify → plan → execute | Done | [#715](https://github.com/sourceplane/orun/pull/715) | v2.73.4 |
| PD6 — Reference sweep and close-out | Done | (this PR) | v2.73.5 |

## Notes

- PD2 found that `.orun/compositions.lock.yaml` records but does not pin, and
  corrected the PD1 pages that said otherwise (v2.73.0 shipped the claim).
- Enforcement gaps found by the review are out of scope for this epic and
  are documented honestly on `concepts/standards.md`: intent and profile
  `policies`, cross-plan promotion gates, `dependsOn.condition`, agent-type
  `mayAffect`, and the write-only composition lock.

- The docs site deploys by hand (`wrangler pages deploy`). Each milestone is
  validated with `npm run docs:build`; publishing the site is left to a
  maintainer with Cloudflare access.
- PD4 left two planned items unchanged after checking them: the skill's
  `v2.58.15 or later` is a minimum version, not a pin, so it stays; and the
  "work plane is gone" comments in `agents/*.md` and `cmd/orun/pr.go` refer to
  the work plane removed in v2.54, not to today's `orun work`, so they are
  accurate.
- PD5 found that `orun plan --name` prints an `orun run <name>` hint that
  does not resolve (the argument is treated as a component name). The docs and
  flag help now describe current behaviour; fixing the hint is out of scope.
- Follow-ups outside this epic's scope: enforce intent and profile `policies`
  (done in v2.75.0: the standards page now labels them enforced); make the composition lock pin or drop it; fix the `orun run <name>` hint
  printed by `orun plan --name`; rename the remaining "Orun Cloud" strings in
  CLI flag help to Orunbase.
