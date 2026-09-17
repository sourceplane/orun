package remotestate

// DefaultCloudURL is where anything that speaks to the platform dials when
// nothing names a backend: not a flag, not ORUN_BACKEND_URL, not intent.yaml,
// not ~/.orun/config.yaml, not a hook's `backendUrl` parameter. It is the LAST
// rung of every such chain, so each explicit source still wins — a self-hosted
// backend, a sandbox whose control plane injected its own URL, a CI job pinned
// to stage.
//
// Before this there was no last rung, and `orun baseline new cirrus` on a
// fresh machine failed with "missing backend URL" before doing anything. The
// platform has one production API; a CLI that makes every user find and type
// its address is a CLI that gets typed wrong.
//
// It lives HERE, below both callers, because the CLI and the hook actions have
// to agree on it. When only the command layer had a default, `orun baseline
// new` reached the platform and the bootstrap it started did not:
//
//	✕ phase "03-infrastructure" precondition "providers" is not met:
//	  hook "providers" (orun.doctor/check@v1): no backend URL: pass
//	  `backendUrl` or set ORUN_BACKEND_URL
//
// The workers.dev hostname, deliberately, for now: `api.orunbase.com` is the
// estate's intended name and now resolves, but pointing every client at it is
// a cutover to make on purpose, not a side effect of this constant moving.
// Flip this one line when that call is made; nothing else needs to change.
const DefaultCloudURL = "https://api-edge-prod.oruncloud.workers.dev"
