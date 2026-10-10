# Script and storyboard

Voice: Kokoro-82M `af_heart`, speed 1.0. "orun" is voiced as "Oh-run".
Word timings: faster-whisper `small.en` → `audio_meta.json`.
Each cue in `index.html` names the word it is locked to.

| # | Line (VO) | What the line does | Proof on screen |
|---|---|---|---|
| 01 | The best engineering teams don't ship faster because they type faster. | The line draws itself alone, then the claim appears above it and the myth below it. The myth is struck through on "faster". | — |
| 02 | They ship on a platform. Years of discipline, that most teams never get to build. | Below the line, the disciplines arrive, then dim on "never". | Concepts from the orun docs: environments, policies, golden paths, dependency order, approvals, change detection, catalog, audit, secrets brokered per run, migrations, preview lanes, RBAC. |
| 03 | orun turns that discipline into intent. What exists. Where it ships. And the rules it lives by. | Each clause highlights its block of YAML. | `examples/infra/infra-1/component.yaml` and the `intent.yaml` production policy, both verbatim. |
| 04 | One open-source binary compiles it into a single plan. Same inputs, same plan. Every time. | **The line sweeps up through the YAML, and what passes it becomes the plan.** Three runs follow, with the digests matching. | Real `orun plan` v2.69.0 output on `examples/`: `source=sha256:eb2c30c…`, `catalog=sha256:edacb30…`, 38 jobs. |
| 05 | Policy runs at compile time. So when an agent breaks a rule, it fails in the plan. Not in production. | **The agent's change hits the line and bounces.** The error prints below. | The real `✕ component validation failed … additionalProperties 'autoApprove' not allowed`, reproduced by adding `autoApprove: true` to the example component. |
| 06 | Want the whole platform? Pick a baseline. Connect your cloud. | Lumen is selected, then providers connect. | The three real baselines from orunbase.com, with their tags, providers and times. |
| 07 | About an hour later: forty-four components, twelve bounded contexts, three environments. Live, in your repo. | **The line sweeps down the frame, and the real system is built behind it.** | The real `lumen` workspace catalog: 45 components and their dependsOn edges. The figures 44 / 12 / 3 are from the Lumen baseline page. |
| 08 | Then every commit converges. Forty-one jobs. All green. | The commit drops through the line, and 41 cells flip in real dependency order. | Real run `1J2YH4YA…` at commit `7835981` on `main`, plan `sha256:8f16e29a…`, 41 of 41 jobs succeeded. |
| 09 | orun. Platform discipline, compiled. Open source. | **The sun rises over the line.** The line becomes the Orunbase mark. | `github.com/sourceplane/orun` · `orunbase.com` |

Note on 07: the live catalog lists 45 Component entities, while the baseline page says 44. The
counter shows the page's published figure, and the graph draws every catalog entity as it is.
