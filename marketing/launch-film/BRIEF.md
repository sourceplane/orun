# orun launch film: brief

Built with the [product-launch-motion](https://github.com/AbubakrChan/product-launch-motion)
discipline, rendered with [HyperFrames](https://github.com/heygen-com/hyperframes) (HTML + GSAP,
seek-based, deterministic). The voice is local Kokoro-82M TTS, the word timings come from Whisper,
and every sound is synthesised in code.

## Intake

| | |
|---|---|
| **Product** | `orun`, the open-source intent compiler for platform engineering, with Orunbase, the hosted control plane and baseline registry. |
| **Audience** | Founders, SREs and platform engineers on X, LinkedIn, the GitHub README and conference screens. Often muted, so on-screen type carries the claim alone. |
| **The one claim** | The platform discipline of a top engineering org, written as intent, compiled deterministically, and live on the infrastructure you choose in about an hour. |
| **Angle** | Borrowed from the founders' own post. Top-tier platform discipline is what gives a product velocity and quality. Kubernetes-style declarative specs make that discipline portable. |
| **Values the film must carry** | Determinism (same inputs, same plan) · policy at compile time · converge on every commit · portable, you own it (your repo, your cloud) · open source, single binary. |

## Approved figures

These are the only numerals allowed on screen:

| Figure | Source |
|---|---|
| 1 binary, MIT, v2.69.0 | README; latest release (2026-09-29) |
| 3 runners (shell · docker · GitHub Actions) | README "backend-swappable runtime" |
| 44 components · 12 bounded contexts · 3 environments | orunbase.com/baselines/lumen |
| ~75 min (Lumen), measured ~73 min to live | orunbase.com/baselines/lumen |
| 8 phases | orunbase.com/baselines/lumen ("Eight phases, in four arcs") |
| Cirrus: 43 components, ~60 min, `baseline-v12` · Multi-tenant SaaS: 44 components, ~75 min, `baseline-v3` · Lumen `baseline-v29` | orunbase.com/baselines |
| 38 jobs · 15 components × 5 envs · `source=sha256:eb2c30c…` · `catalog=sha256:edacb30…` | Real `orun plan` (v2.69.0) output on this repo's `examples/`, identical across repeated runs |
| 45 catalog components, including 17 test lanes | `lumen` workspace catalog (drawn, not stated) |
| 41 / 41 jobs succeeded | Real run `1J2YH4YABW53MCRG8G9291HYJA` in the `lumen` Orunbase workspace, commit `7835981`, `refs/heads/main` |
| Real catalog names and edges (`api-edge` → 14 dependencies, workers, `supabase`, `cloudflare-kv`, …) | `lumen` workspace catalog, commit `bc49307` |

These are refused:

- An AWS baseline. None exists yet, so the film says "Cloudflare + Supabase" and "Cloudflare".
- Customer quotes. The homepage and the login page credit the same quote to two different people, so no testimonial is used.
- Third-party company names as endorsements.
- A matching plan ID across runs. `orun plan` prints a different plan ID and `metadata.checksum` on each run, because job order in `plan.json` is not stable. The film shows only what really is identical: the source and catalog digests, and the 38-job set. The ordering is tracked as a bug.

## Directions (three written, two killed)

**A · "Terminal as theatre"**: near-black, all mono, one continuous scrolling terminal.
*Killed:* it is worked direction #1 in the skill, inherited rather than derived, and a
competitor could use it unchanged.

**B · "Skyline"**: a deep 3D camera flies over the real 44-component graph rising like a city.
*Killed:* it serves scale but not *discipline*. It says "big", not "correct", and it fails badly
on a bad day, because a 3D graph at 70% execution reads as a screensaver.

**C · "Above the line" (chosen)**
- **Thesis.** Intent lives above the line. The platform exists below it. `orun` is the line.
- **Dials.** Energy medium-fast, with long holds on the hero beats. Density starts sparse and
  peaks at the graph. Ground is the void (`#08090a`). Depth is flat, with one tilt on the graph.
  Camera is locked, and moves are vertical pans across the line. Type is Inter 700 at −4%
  tracking, with JetBrains Mono for anything the machine says. Texture is a faint grain.
  Colour is monochrome plus the brand amber. Product is shown through real CLI output and the
  real catalog. Sound is design-led, with a synth bed. Voice narrates throughout.
- **Palette.** Ground `#08090a`, ink `#f7f8f8`, secondary ink `#9aa0a9`, accent `#d9b45c` /
  `#B8862F` (the site's own `--ac` / `--ac-deep`), plus the status colours `--ok #4cc38a` and
  `--bad #e5675a`, which are allowed only where the product itself prints them.
- **Signature move.** The amber horizon line from the Orunbase mark, the gap between the sun and
  its reflection, is the compiler.
  1. It opens the film alone.
  2. It compiles YAML into a plan.
  3. It *rejects* a non-compliant change, which bounces off it.
  4. It sweeps a baseline into existence.
  5. It closes the film by becoming the logo's horizon as the sun rises.
- **Accent budget.** The line itself, one emphasis word per headline, the stat numerals, and
  the CTA.
- **Risk.** The line stops meaning anything if it becomes decoration, so every appearance must
  *do* something.

## Structure (checked against defaults)

- **Device.** Transformation: intent in, platform out, and the line does the work.
- **Turn.** At ~20%, early. The film spends its time on the evidence, not the problem.
- **Ending.** The film ends on the product in use, not a quiet drain. A new commit arrives and
  the plan recompiles, and only then does the sun rise.
