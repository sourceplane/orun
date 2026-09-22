---
title: Deploying docs
description: How this Docusaurus site is built, validated, and published to Cloudflare Pages by hand.
---

The `orun` documentation site is a docs-only Docusaurus project under `website/`. The production target is Cloudflare Pages, and the canonical URL is `https://orun-docs.pages.dev` (`url` in `website/docusaurus.config.js`, served at the root path).

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
npm run docs:build     # docusaurus build --out-dir docs-build
npm run docs:serve     # serve docs-build/ locally
```

The static output is written to `website/docs-build/`. Both `onBrokenLinks` and `onBrokenMarkdownLinks` are set to `throw`, so a broken internal link fails the build — treat a green build as the link check.

## Deploy

There is no CI workflow for the docs today; the workflows under `.github/workflows/` cover the CLI release, conformance, and test suites only. Deploys are manual:

```bash
cd website
npm ci
npm run docs:build
wrangler login
wrangler pages deploy docs-build --project-name orun-docs
```

Replace `orun-docs` if your Cloudflare Pages project name differs.

## Tracking what the docs cover

`website/.docs-last-version` used to record the release the docs were last refreshed against and what that refresh covered. It is being replaced by the release notes under `website/docs/release-notes/`: when you document a release, add its page there and update the navbar link in `docusaurus.config.js`.

## Notes

- `docs-build/` is generated output, not source of truth
- update `docusaurus.config.js` if the public site URL changes
- new pages need an entry in `website/sidebars.js` to appear in the navigation

## Related

- [Contributing](./contributing.md) — the wider development loop.
