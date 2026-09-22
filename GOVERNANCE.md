# Governance

This document describes how the orun project is governed: who makes decisions,
how contributors become maintainers, and how disagreements are resolved. It is
deliberately lightweight for a project of this size and will be revised as the
community grows.

## Principles

- **Open.** Design discussions, decisions, and roadmaps happen in public, in
  this repository's issues, discussions, pull requests, and `specs/`.
- **Transparent.** Decisions are recorded where they are made. A merged pull
  request or an updated spec is the record of a decision.
- **Meritocratic.** Influence is earned through sustained, high-quality
  contribution, not through employer or seniority.
- **Vendor neutral.** orun is an open-source engine. The hosted Orunbase
  service is one consumer of it, and decisions about the engine are made on
  the engine's merits.

## Roles

### Contributors

Anyone who opens an issue, comments on a design, submits a pull request, or
improves documentation is a contributor. Contributors must follow the
[code of conduct](CODE_OF_CONDUCT.md) and sign off their commits as described
in [CONTRIBUTING.md](CONTRIBUTING.md).

### Reviewers

Reviewers are contributors who have demonstrated good judgement in one or more
areas of the codebase and are trusted to review pull requests there. A reviewer
approval carries weight but does not by itself merge a change.

### Maintainers

Maintainers have write access to the repository. They review and merge pull
requests, triage issues, cut releases, and are accountable for the project's
health. The current maintainers are listed in [MAINTAINERS.md](MAINTAINERS.md).

Maintainers are expected to:

- review contributions in a timely, constructive manner;
- keep the project's invariants (determinism, fail-closed behaviour, the
  separation of intent from execution) intact;
- participate in design discussions and the release process;
- uphold the code of conduct.

## Becoming a maintainer

A contributor may be nominated as a maintainer by an existing maintainer after
a sustained period of contribution, typically several months, that shows:

- a series of merged, non-trivial pull requests;
- reviews of other people's changes that improved them;
- an understanding of the project's design and invariants;
- respectful, collaborative behaviour.

Nominations are made in a GitHub issue. The nomination is accepted when a
majority of existing maintainers approve it and none object within seven days.

## Stepping down and removal

Maintainers may step down at any time by opening a pull request that removes
them from `MAINTAINERS.md`. A maintainer who has been inactive for six months
may be moved to emeritus status by a majority of the remaining maintainers.
A maintainer may be removed for violating the code of conduct or repeatedly
acting against the project's interests by a two-thirds vote of the other
maintainers.

## Decision making

Most decisions are made by lazy consensus on a pull request or issue: a change
is accepted when it has the required approval and no maintainer has objected
within a reasonable review window.

Decisions with wide impact, such as breaking changes to the intent schema,
`plan.json`, the `.orun/` object model, or the addition of a new top-level
command family, start as a spec under `specs/` and are discussed there before
implementation begins.

When consensus cannot be reached, any maintainer may call a vote. Votes are
held in the open on the relevant issue, last at least seven days, and pass by
simple majority of maintainers unless this document specifies otherwise.

## Changes to governance

This document is changed by pull request, approved by a two-thirds majority
of maintainers.
