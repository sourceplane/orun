# orun-cache-efficiency — implementation status

| Milestone | Status | Scope |
|---|---|---|
| **CE0** | ✅ #770 | crash-safe action-cache lock with waits as long as the download; the download bounded per phase; `npm_config_store_dir` exported; the cache-hit signal taken from the tool cache, not from action output; `.orun/runs/<exec>` removed at run end |
| **CE1** | ✅ #771 | the action tarball fetched by the resolved SHA; ref entries carry an age (tags 24 h, branches 1 h, SHAs immutable); `orun run --refresh-actions` |
| **CE2** | ✅ #772 | `flock` across processes for the tool cache and the Terraform plugin-cache slots |
| **CE3** | ✅ #774 | the host's `RUNNER_TOOL_CACHE` inherited, `ORUN_TOOL_CACHE`; a shared cache home and the common toolchains' cache variables; `caches:` declared on composition steps |
| **CE4** | ✅ #775 | the turbo cache per checkout (`.orun/cache/turbo`); setup actions once per lane |
| **CE5** | ✅ #776 | `dir` composition sources loaded in place; locked archive and OCI fills; `orun cache ls / prune / gc` with a size budget over every cache |
| **CE6** | ✅ #777 | the memo key over the source digest and the composition lock; hits counted in the summary; a hit restores nothing, documented (outputs in the result and their restoration stay out of scope: the platform's result object carries none) |
| **CE7** | ⏳ | native `actions/cache`, `actions/cache/restore` and `actions/cache/save` against a store under the shared cache directory, listed by `orun cache`; a remote backing by the platform's object store stays a follow-up |

The findings each milestone answers, and the measurements behind them, are in
[README.md](README.md) §4 and §3.
