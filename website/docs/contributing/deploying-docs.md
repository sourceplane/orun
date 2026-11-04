---
title: Deploying docs
description: How this Docusaurus site is built, validated, and published to Cloudflare Pages by hand.
---

The `orun` documentation site is a docs-only Docusaurus project under `website/`. It is published to the `orun-docs` Cloudflare Pages project and read at **`https://orun.dev/docs/`** (`url` and `baseUrl` in `website/docusaurus.config.js`).

Three pieces make that address work:

- **The build** is written under `docs-build/docs/`, so the Pages project serves every page at the same `/docs/…` path it has on orun.dev.
- **orun.dev's Worker** (`apps/website/worker/` in `sourceplane/orun-cloud`) forwards `/docs/*` to the Pages project and marks each request with an `x-orun-docs-proxy` header.
- **`website/functions/_middleware.js`** 301s every request without that mark to the same page on orun.dev. Anyone reaching `orun-docs.pages.dev` directly, including links from before the move such as `orun-docs.pages.dev/cli/orun/`, lands on `orun.dev/docs/cli/orun/`.

## Local validation

Install dependencies and run the local dev server:

```bash
cd website
npm ci
npm run docs:start
```

Create a production build:

```bash
cd website
npm ci
npm run docs:build     # docusaurus build --out-dir docs-build/docs
npm run docs:serve     # serve it locally at http://localhost:3000/docs/
```

The static output is written to `website/docs-build/docs/`. Both `onBrokenLinks` and `onBrokenMarkdownLinks` are set to `throw`, so a broken internal link fails the build — treat a green build as the link check.

## Deploy

There is no CI workflow for the docs today; the workflows under `.github/workflows/` cover the CLI release, conformance, and test suites only. Deploys are manual:

```bash
cd website
npm ci
npm run docs:build
wrangler login
wrangler pages deploy docs-build --project-name orun-docs --branch main
```

Run it from `website/`: `wrangler` uploads `functions/` from the directory it runs in, and without the middleware the pages.dev host would serve the docs again instead of redirecting. Replace `orun-docs` if your Cloudflare Pages project name differs (and the proxy's origin in orun.dev's Worker with it).

## Tracking what the docs cover

`website/.docs-last-version` used to record the release the docs were last refreshed against and what that refresh covered. It is being replaced by the release notes under `website/docs/release-notes/`: when you document a release, add its page there and list it first in the release-notes sidebar. The `Releases` navbar link points at the GitHub releases page and needs no update.

## Notes

- `docs-build/` is generated output, not source of truth
- update `docusaurus.config.js`, the middleware and orun.dev's Worker together if the public URL changes
- new pages need an entry in `website/sidebars.js` to appear in the navigation

## Related

- [Contributing](./contributing.md) — the wider development loop.
