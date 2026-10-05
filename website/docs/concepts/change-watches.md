---
title: Change watches
description: Change watches (spec.change.watches) let a component opt in to being re-planned when a section of intent.yaml changes. They are now documented on the change detection page.
---

`spec.change.watches` lets a component opt in to being marked changed when a section of
`intent.yaml` changes, such as `environments` or `groups`. Without watches, a change to a
global intent section does not re-plan the component.

Watches, their valid values, the `--intent-impact` flag, and how intent changes combine
with file changes and dependencies are documented in
[change detection](./change-detection.md#intent-aware-change-scoping).

Note one correction to what this page used to say: a component that merely depends on a
changed component is in the change's blast radius, but it is **not** planned unless the
edge is a build input (`input: true`) or the dependency is `include: always`. See
[selection and blast radius](./change-detection.md#selection-and-blast-radius).
