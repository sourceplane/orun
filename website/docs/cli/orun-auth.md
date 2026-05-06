---
title: orun auth
---

`orun auth` manages the local CLI session used for remote state outside GitHub Actions.

## Commands

```bash
orun auth login
orun auth login --device
orun auth status
orun auth logout
orun auth token --audience orun-backend
```

## Login

Interactive browser login:

```bash
orun auth login --backend-url https://orun-api.example.workers.dev
```

Headless device login:

```bash
orun auth login --device --backend-url https://orun-api.example.workers.dev
```

The CLI stores only Orun-issued access and refresh tokens. It does not store GitHub OAuth access tokens or GitHub PATs.

## Status

```bash
orun auth status
```

Shows:

- GitHub login
- backend URL
- access-token expiry
- whether the current Git remote is linked for local remote-state runs

## Logout

```bash
orun auth logout
```

Revokes the backend refresh token when available, then removes local credentials.

## Token

```bash
orun auth token --audience orun-backend
```

Prints the current short-lived Orun access token. This is intended for explicit debugging or automation handoff.

## Storage

- Prefer the OS credential store when available
- Fallback to `~/.orun/credentials.json` with `0600` permissions
- Store non-secret config in `~/.orun/config.yaml`
