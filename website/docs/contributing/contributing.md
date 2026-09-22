---
title: Contributing
description: Where the contribution rules live, the make targets that make up the development loop, and the DCO sign-off every commit needs.
---

`orun` is an open-source Go CLI. The rules of the project live in the repository, not on this site — this page tells you where they are and summarises the loop you will run every day.

## Project documents

| Document | What it covers |
| --- | --- |
| [CONTRIBUTING.md](https://github.com/sourceplane/orun/blob/main/CONTRIBUTING.md) | The full contributor guide: environment, the development loop, project invariants, commit messages, pull requests, design proposals, releases |
| [CODE_OF_CONDUCT.md](https://github.com/sourceplane/orun/blob/main/CODE_OF_CONDUCT.md) | Applies to every interaction in the project |
| [SECURITY.md](https://github.com/sourceplane/orun/blob/main/SECURITY.md) | How to report a vulnerability — never through a public issue |
| [GOVERNANCE.md](https://github.com/sourceplane/orun/blob/main/GOVERNANCE.md) | How decisions are made and who makes them |

Search the [issue tracker](https://github.com/sourceplane/orun/issues) before opening an issue, and open one first for anything larger than a bug fix.

## Development loop

You need Go 1.25 or later, and Node 20 or later if you work on this site. The `Makefile` targets are the loop:

| Target | What it runs |
| --- | --- |
| `make build` | `go build -o orun ./cmd/orun` |
| `make test` | `go test -v ./...` |
| `make lint` | `go vet ./...` |
| `make fmt` | `go fmt ./...` |
| `make verify-generated` | Regenerates the catalog schema under `internal/catalogmodel/schema/` and fails if it drifted from what is committed |
| `make test-object-model` | The object-model lint gate plus per-package coverage thresholds for `objectstore`, `nodes`, `nodewriter`, `objplan`, `workingview`, `execseal`, `runworktree`, `objread`, `objindex`, `objgc`, `objremote`, and the end-to-end walk |
| `make test-state-redesign` | The state-redesign, catalog, and unified-MCP suites with their coverage gates |
| `make tui-bench` | Cockpit v2 render benchmarks |
| `make release-snapshot` | `goreleaser build --snapshot --clean` into `dist/` |

`make help` lists everything. While iterating, run one package's tests directly (`go test ./internal/scaffold/...`), and exercise a change end to end against the working platform intent under `examples/`.

To preview this site:

```bash
cd website
npm ci
npm run docs:start
```

## Sign-off

Every commit carries a Developer Certificate of Origin sign-off, the same certification used across CNCF projects. Add it with:

```bash
git commit -s
```

which appends `Signed-off-by: Your Name <you@example.com>`. The full text of the certificate is at [developercertificate.org](https://developercertificate.org).

## What reviewers look for

- **Determinism.** `orun plan` is a pure function of its inputs; never read the clock, the network, or the environment inside a compiler stage.
- **Intent and execution stay separate.** Intent files say *what*; the compiler and the runner decide *how*.
- **Fail closed.** A scaffold, plan, or catalog that does not validate is an error, never a half-written result.
- **Thin command handlers.** `cmd/orun` wires Cobra to packages under `internal/`, where the logic is tested without a terminal.
- **One design language.** CLI, TUI, and JSON output render through the cockpit view-model in `internal/cockpit`.
- **Tests travel with the change**, and **secrets never touch disk**.

## Related

- [Extending orun](./extending-orun.md) — where to add a command, a driver, an MCP tool provider, an object kind, or a workflow action.
- [Deploying docs](./deploying-docs.md) — building and publishing this site.
- [Internals](../architecture/internals.md) — the package map.
