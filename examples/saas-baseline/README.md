# saas-baseline

The baseline from the README recording. Try the same loop yourself: build a
product from a baseline, then plan it and run it.

| Path | What it is |
|---|---|
| `blueprint.yaml` | How `orun new` builds a product: one input (`name`), then two phases |
| `platform/stack/` | **standards** phase: Acme's golden path for Node services, the `node-service` composition. `test`, `build` and `package` are plain shell steps, so a local run does real work |
| `product/` | **product** phase: the intent (staging, then production) and two services, `api` and `web`, where `web` depends on `api` |

Services ship to staging with the `verify` profile (test, build) and to
production with `release` (test, build, package). Production waits for
staging.

## Try it

You need `orun` on your `PATH` and Node.js 18 or later for the services' tests.

```bash
cd examples
orun new --blueprint saas-baseline/blueprint.yaml --out acme-shop --set name=acme-shop
cd acme-shop
orun plan
orun run <id>          # the id `orun plan` printed after "orun run"
```

`orun new` copies the golden path, renders the intent with your `name`, then
checks that the new repository plans. It also writes
`.orun/provenance.lock`, which records the files' origin. `orun plan` compiles
four jobs: two services in two environments. `orun run` runs them locally,
each in a staged copy of the repository, and prints the `orun status` and
`orun logs` commands for the run.

To start over, delete `acme-shop/`; it is git-ignored. To see everything
`orun new` placed, add `--progress verbose`.

## Learn more

- [Baselines](https://orun-docs.pages.dev/concepts/baselines): package a product's structure and standards, and evolve products built from them.
- [`orun new`](https://orun-docs.pages.dev/cli/orun-new): blueprints, inputs, modules and phases.
