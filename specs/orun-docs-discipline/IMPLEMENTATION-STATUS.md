# orun-docs-discipline — Implementation status

| Milestone | Status | PR | Release |
|---|---|---|---|
| PD1 — The narrative: tagline, landing page, information architecture | Done | [#710](https://github.com/sourceplane/orun/pull/710) | v2.73.0 |
| PD2 — Pillar 1, Declare | Done | [#711](https://github.com/sourceplane/orun/pull/711) | v2.73.1 |
| PD3 — Pillar 2, Package & evolve | Done | (this PR) | v2.73.2 |
| PD4 — Pillar 3, Ground agents | Not started | | |
| PD5 — Pillar 4, Verify → plan → execute | Not started | | |
| PD6 — Reference sweep and close-out | Not started | | |

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
