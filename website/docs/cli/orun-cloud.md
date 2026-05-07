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

1. Ensures the CLI is logged in (requires `orun auth login`)
2. Detects the current GitHub remote (`git remote get-url origin`)
3. Calls `POST /v1/accounts/repos/link` with the CLI session token — the backend resolves the repo to a namespace ID from slug data recorded during login
4. Stores the backend URL and repo namespace ID in `~/.orun/config.yaml`

No GitHub PAT or OAuth token is required.  The Orun CLI session from `orun auth login` is sufficient.

### Auto-resolve vs explicit link

`orun run --remote-state` **auto-resolves** the namespace ID on the first run outside GitHub Actions:

- If a cached link already exists in `~/.orun/config.yaml`, it is used immediately.
- If no cached link exists, the CLI calls `POST /v1/accounts/repos/link` automatically, then persists the result.

`orun cloud link` is therefore optional for interactive use.  Run it explicitly to:

- Pre-cache the namespace link before non-interactive scripts
- Diagnose or reset the cached link for the current repo
- Inspect which namespace was resolved (`namespace:` line in output)

### Error handling

| Backend response | Message | Fix |
|---|---|---|
| `NOT_FOUND` | Repo not known to session | Run `orun auth login` again to refresh namespace access |
| `FORBIDDEN` | Repo not authorized in session | Re-authenticate with `orun auth login` or verify GitHub admin access |
| `UNAUTHORIZED` | Session expired | Run `orun auth login` |

### ORUN_TOKEN

When `ORUN_TOKEN` is set, the CLI cannot call the session link endpoint (it is not a CLI session token).  Pre-cache the namespace link with `orun cloud link` before using `ORUN_TOKEN` for remote-state runs.
