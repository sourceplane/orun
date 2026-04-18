---
title: Contributing
---

Contributions to `arx` should preserve deterministic planning, clear stage boundaries, and thin command handlers.

## Local development loop

Build the CLI:

```bash
make build
```

Run the main test suites:

```bash
go test ./...
cd scripts/releaser && go test ./...
```

Preview the docs site:

```bash
cd website
npm install
npm run docs:start
```

## Contribution areas

- new composition types
- planner and runner improvements
- GitHub Actions compatibility coverage
- documentation and examples
- packaging and release automation

## Review expectations

- prefer root-cause fixes over output-only tweaks
- keep compile behavior deterministic
- avoid mixing Cobra wiring with business logic
- update docs when CLI behavior or contracts change

Read [extending arx](./extending-arx.md) if your change affects commands, stages, or runtime backends.