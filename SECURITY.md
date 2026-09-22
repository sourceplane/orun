# Security Policy

The orun maintainers take security seriously. orun runs inside CI systems and
on developer machines with access to deployment credentials, so we treat any
weakness in how it handles secrets, plans, or remote state as high priority.

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.**

Report them privately through GitHub's vulnerability reporting form:

<https://github.com/sourceplane/orun/security/advisories/new>

Include as much of the following as you can:

- The version of orun (`orun version`) and the platform you observed it on.
- The command, intent, composition, or plan that triggers the problem, reduced
  to the smallest reproduction you can manage.
- What an attacker could achieve: for example, reading a secret, running
  arbitrary commands from an untrusted composition, or escaping the scaffold
  sandbox.
- Any suggested fix.

You will receive an acknowledgement within three business days. We will keep
you informed as we triage, fix, and disclose the issue, and we will credit you
in the advisory unless you ask us not to.

## Supported versions

Security fixes are released for the latest minor version on the `v2` line.
Older versions are not patched; upgrade to the latest release to receive
fixes.

| Version | Supported |
|---|---|
| Latest `v2.x` release | Yes |
| Earlier `v2.x` releases | No, upgrade |
| `v1.x` and earlier | No |

## What is in scope

- The `orun` CLI and everything under `internal/`.
- The scaffold engine's sandbox (`orun new`, `orun baseline new --local`),
  including template rendering, path containment, and the secret sweep.
- Handling of `ORUN_TOKEN`, the CLI session store, and the OS credential store.
- Plan and object-model integrity under `.orun/`.
- The install script and release artifacts.

Vulnerabilities in the hosted Orunbase control plane should be reported to the
[orun-cloud](https://github.com/sourceplane/orun-cloud) repository's security
form instead.

## Disclosure process

1. The report is acknowledged and triaged privately.
2. A fix is developed on a private branch with a regression test.
3. A patch release is published, and a GitHub security advisory is issued
   with a CVE where appropriate.
4. The advisory credits the reporter and describes affected versions and
   mitigations.

## Verifying release artifacts

Every release publishes a `checksums.txt` alongside the archives. Verify a
download before installing it:

```bash
VERSION=v2.58.15
curl -fsSLO "https://github.com/sourceplane/orun/releases/download/${VERSION}/checksums.txt"
curl -fsSLO "https://github.com/sourceplane/orun/releases/download/${VERSION}/orun_${VERSION#v}_linux_amd64.tar.gz"
shasum -a 256 --check --ignore-missing checksums.txt   # or: sha256sum --check --ignore-missing checksums.txt
```
