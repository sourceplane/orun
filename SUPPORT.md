# Support

This page explains where to get help with orun and what to include so that
people can help you quickly.

## Where to ask

| Need | Go to |
|---|---|
| A question about how something works | [GitHub Discussions](https://github.com/sourceplane/orun/discussions) |
| A bug in the CLI, the compiler, a runner, or the docs | [Open a bug report](https://github.com/sourceplane/orun/issues/new?template=bug_report.yml) |
| A feature or design idea | [Open a feature request](https://github.com/sourceplane/orun/issues/new?template=feature_request.yml) |
| A security vulnerability | [SECURITY.md](SECURITY.md), never a public issue |
| The hosted Orunbase service (console, API, billing, workspaces) | [docs.orunbase.com](https://docs.orunbase.com) and the [orun-cloud](https://github.com/sourceplane/orun-cloud) repository |

## Before you ask

- Check the [documentation](https://orun-docs.pages.dev). The CLI reference and
  the concept pages answer most "how do I" questions.
- Run `orun <command> --help`. The help text is generated from the same code
  that runs, so it is always current.
- Upgrade to the latest release and confirm the problem still occurs.

## What to include

- The output of `orun version`.
- Your operating system and architecture.
- The exact command you ran and its complete output. Set `NO_COLOR=1` to
  strip colour codes when pasting.
- For compiler problems, the smallest `intent.yaml` and `component.yaml` that
  reproduce it. `orun debug --intent <file>` prints every compiler stage and is
  usually the fastest way to locate where a plan goes wrong.
- For runner problems, the `plan.json` and the output of `orun logs --failed`.
- For cloud problems, the output of `orun auth status`, `orun workspace`, and
  `orun cloud status`. These never print secrets.

## Response expectations

orun is maintained by a small team. Issues are triaged regularly, but there is
no guaranteed response time. Pull requests that include a fix and a test are
the fastest way to get a problem resolved.
