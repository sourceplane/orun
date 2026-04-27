---
title: Run with Docker
---

The Docker backend executes each step inside a container and mounts the repository workspace at `/workspace`.

## Compile a plan first

```bash
gluon plan \
  --intent examples/intent.yaml \
  --output /tmp/gluon-docker-plan.json
```

## Run through Docker

```bash
gluon run \
  --plan /tmp/gluon-docker-plan.json \
  --runner docker
```

## Runner behavior

- `job.runsOn` is treated as the container image
- GitHub-style labels such as `ubuntu-22.04` map to a default Ubuntu image
- the workspace is mounted at `/workspace`
- the job working directory is resolved inside that mount

Use the Docker backend when you want stronger isolation than the local shell but do not need GitHub Actions semantics.
