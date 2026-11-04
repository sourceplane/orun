# website/ — the retired docs host

The orun docs are **https://orun.dev/docs/**. Their source moved to
[sourceplane/orun-website](https://github.com/sourceplane/orun-website)
(`apps/web/content/docs/`), the repository that serves orun.dev — the landing
page, the installer and the docs — and deploys on every merge to its `main`.

What is left here is the old host, `orun-docs.pages.dev` (the `orun-docs`
Cloudflare Pages project), turned into a redirect so that every link to it —
in search results, old READMEs, issues — keeps working:
`functions/_middleware.js` 301s every path to the same page under
`https://orun.dev/docs/`, query string included (`/cli/orun/` →
`/docs/cli/orun/`). `public/` holds a one-line page the middleware never lets
anyone see; Pages needs a directory to deploy.

Publish it once, by hand, from this directory (the project has no CI):

```bash
cd website
wrangler login
wrangler pages deploy public --project-name orun-docs --branch main
```

`wrangler` uploads `functions/` from the directory it runs in, so run it here.
