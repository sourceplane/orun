# Contributing to orun

Thank you for your interest in contributing. orun is an open-source intent
compiler for platform engineering, and every kind of contribution helps: bug
reports, documentation fixes, new compositions, runner backends, and design
proposals.

This document explains how the project works day to day. The
[governance](GOVERNANCE.md) document explains how decisions are made, and the
[code of conduct](CODE_OF_CONDUCT.md) applies to every interaction.

## Table of contents

- [Before you start](#before-you-start)
- [Development environment](#development-environment)
- [The development loop](#the-development-loop)
- [Making a change](#making-a-change)
- [Commit messages and sign-off](#commit-messages-and-sign-off)
- [Pull requests](#pull-requests)
- [Documentation](#documentation)
- [Design proposals and specs](#design-proposals-and-specs)
- [Release process](#release-process)
- [Getting help](#getting-help)

## Before you start

- Search the [issue tracker](https://github.com/sourceplane/orun/issues)
  before opening a new issue. If you find a matching one, add a reaction or a
  comment rather than a duplicate.
- For anything larger than a bug fix or a small improvement, open an issue
  first and describe the problem you want to solve. A short conversation up
  front saves a long review later.
- Security problems are never reported through public issues. Follow
  [SECURITY.md](SECURITY.md).

## Development environment

| Requirement | Version | Used for |
|---|---|---|
| Go | 1.25 or later (see `go.mod`) | Building and testing the CLI |
| Node.js and npm | Node 20 or later | Building the documentation site under `website/` |
| Docker | any recent release | Only when working on the Docker runner |
| `kiox` | optional | Running orun as a pinned OCI provider |

Clone the repository and build the binary:

```bash
git clone https://github.com/sourceplane/orun.git
cd orun
make build
./orun version
```

`make help` lists every target.

## The development loop

```bash
make build            # go build -o orun ./cmd/orun
make test             # go test -v ./...
make lint             # go vet ./...
make fmt              # go fmt ./...
make verify-generated # regenerate the catalog schema and fail if it drifted
make examples-plan    # compile the example intent under examples/
```

Run a single package's tests while iterating:

```bash
go test ./internal/scaffold/...
```

The `examples/` directory is a complete, working platform intent. Most changes
to the compiler, the runner, or the catalog can be exercised end to end with:

```bash
./orun validate --intent examples/intent.yaml
./orun plan --intent examples/intent.yaml --view dag
./orun run --plan .orun/plans/latest.json --dry-run
```

## Making a change

Keep these project invariants in mind. Reviewers will ask about them.

- **Determinism.** `orun plan` is a pure function of its inputs. Identical
  inputs must produce byte-identical plans. Never read the clock, the
  network, or the environment inside a compiler stage.
- **Intent and execution stay separate.** Intent files say *what*; the
  compiler and the runner decide *how*. Do not add execution logic to schemas
  or intent parsing.
- **Fail closed.** A scaffold, a plan, or a catalog that does not validate is
  an error, never a half-written result reported as success.
- **Thin command handlers.** `cmd/orun` wires Cobra commands to packages under
  `internal/`. Business logic belongs in `internal/`, where it can be tested
  without a terminal.
- **One design language.** CLI, TUI, and JSON output all render through the
  cockpit view-model in `internal/cockpit`. Do not hand-format status in a
  command.
- **Tests travel with the change.** Bug fixes include a regression test.
  New behaviour includes tests at the package level, and a golden-file or
  end-to-end test when the behaviour is user visible.
- **Secrets never touch disk.** Anything marked secret is redacted from logs,
  provenance, and generated files. Tests must not read the developer's
  keychain.

## Commit messages and sign-off

Write the subject line as a sentence that describes the behaviour change from
the user's point of view, for example:

```
A resumed build says an earlier run placed a phase, instead of "not needed"
```

Reference the issue in the body when there is one. Keep the subject under
about 80 characters and wrap the body at 72.

Every commit must carry a Developer Certificate of Origin sign-off. This is
the same certification used across CNCF projects: by signing off you state
that you wrote the change or have the right to submit it under the project
licence. Add it with `git commit -s`, which appends:

```
Signed-off-by: Your Name <you@example.com>
```

The full text of the certificate is at <https://developercertificate.org>.

## Pull requests

1. Fork the repository and create a branch from `main`.
2. Make your change, with tests and documentation.
3. Run `make fmt lint test` locally. The release workflow runs `go test ./...`
   and will not publish a release from a failing tree.
4. Open the pull request against `main` and fill in the template. Explain
   what changed and why, and how you verified it.
5. A maintainer reviews it. Address feedback with additional commits; the
   merge squashes or preserves history at the maintainer's discretion.

Small, focused pull requests are reviewed faster than large ones. If a change
touches several subsystems, split it into a sequence where each step is
reviewable on its own.

## Documentation

User-facing documentation lives in two places:

- `website/docs/` is the documentation site (Docusaurus). It is the source of
  truth for concepts, guides, and the CLI reference.
- `README.md` is the front door. It should stay short and link into the site.

When a change alters a command, a flag, an exit code, a file format, or an
environment variable, update the matching page under `website/docs/` in the
same pull request. Add a release-notes entry under
`website/docs/release-notes/` for user-visible changes.

Build the site locally before pushing; broken links fail the build:

```bash
cd website
npm ci
npm run docs:build
```

`docs/` at the repository root holds long-form architecture and design
documents for contributors; see [docs/README.md](docs/README.md).

## Design proposals and specs

Larger changes start as a spec under `specs/<name>/`. A spec directory holds a
`README.md` with the problem, the proposed model, and the milestones, and is
updated as the work lands. Open a pull request with the spec first, get it
reviewed, then implement it milestone by milestone with the spec as the shared
reference. Existing specs are the best examples of the expected depth.

## Release process

Releases are cut by maintainers from `main`.

1. The tree must be green on `main`.
2. Push a tag of the form `vX.Y.Z`, or trigger the `release-oci` workflow by
   hand with the version as input; the workflow creates the tag if it does
   not exist.
3. The workflow runs the tests, builds the binaries with GoReleaser for
   `linux` and `darwin` on `amd64` and `arm64`, publishes them as a GitHub
   release with a `checksums.txt`, and pushes the kiox provider image to
   `ghcr.io/sourceplane/orun:vX.Y.Z`.
4. Release notes for the version are added under
   `website/docs/release-notes/` and linked from `website/sidebars.js`.

The project follows [semantic versioning](https://semver.org). Breaking
changes to intent schemas, `plan.json`, or the `.orun/` object model bump the
major version.

## Getting help

- [GitHub Discussions](https://github.com/sourceplane/orun/discussions) for
  questions and ideas.
- [GitHub Issues](https://github.com/sourceplane/orun/issues) for bugs and
  feature requests.
- See [SUPPORT.md](SUPPORT.md) for what to include when asking for help.
