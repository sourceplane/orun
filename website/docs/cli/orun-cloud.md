---
title: orun cloud
---

`orun cloud` manages local Orun Cloud linkage for the current repository.

## Commands

```bash
orun cloud link
```

## Link the current repo

```bash
orun cloud link --backend-url https://orun-api.example.workers.dev
```

`orun cloud link`:

1. Ensures the CLI is logged in
2. Detects the current GitHub remote
3. Checks whether the repo is already linked in the current Orun account
4. Stores the backend URL and repo namespace ID in `~/.orun/config.yaml`

Local `orun run --remote-state` uses that stored repo link to tell the backend which namespace the run belongs to.
