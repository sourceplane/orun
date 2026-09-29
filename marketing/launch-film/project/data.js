/* Real data. Source: Orunbase `lumen` workspace (ws_4975BJ7P), catalog at commit bc49307,
   and run 1J2YH4YABW53MCRG8G9291HYJA (commit 7835981, refs/heads/main, 41/41 succeeded). */
window.LUMEN = {
  // component -> dependsOn (catalog relations, verbatim)
  deps: {
    "supabase": [], "cloudflare-kv": [], "cloudflare-hyperdrive": ["supabase"],
    "db-migrate": ["db", "supabase"], "cloudflare-domain": ["web-console-next"],
    "contracts": [], "db": [], "shared": [], "policy-engine": [], "sdk": [], "cli": [],
    "notifications-client": [], "webhook-verifier": [], "testing": [],
    "policy-worker": ["contracts", "policy-engine"],
    "billing-worker": ["contracts", "db", "policy-worker"],
    "membership-worker": ["billing-worker", "contracts", "db", "notifications-client", "policy-worker"],
    "events-worker": ["contracts", "db", "membership-worker", "policy-worker"],
    "notifications-worker": ["contracts", "db", "db-migrate", "events-worker"],
    "identity-worker": ["cloudflare-hyperdrive", "contracts", "db", "membership-worker", "notifications-client", "notifications-worker", "policy-worker"],
    "projects-worker": ["billing-worker", "contracts", "db", "membership-worker", "policy-worker"],
    "integrations-worker": ["billing-worker", "contracts", "db", "membership-worker", "policy-worker", "projects-worker"],
    "metering-worker": ["contracts", "db", "membership-worker", "policy-worker"],
    "config-worker": ["contracts", "db", "membership-worker", "policy-worker"],
    "webhooks-worker": ["contracts", "db", "membership-worker", "policy-worker"],
    "admin-worker": ["cloudflare-hyperdrive", "contracts", "db"],
    "api-edge": ["billing-worker", "cloudflare-hyperdrive", "cloudflare-kv", "config-worker", "contracts", "db", "events-worker", "identity-worker", "integrations-worker", "membership-worker", "metering-worker", "notifications-worker", "projects-worker", "webhooks-worker"],
    "web-console-next": ["api-edge", "contracts", "sdk"]
  },
  tests: ["web-console-next-tests", "projects-worker-tests", "policy-worker-tests", "policy-engine-tests",
    "platform-limits-tests", "notifications-worker-tests", "notifications-client-tests", "metering-worker-tests",
    "membership-worker-tests", "integrations-worker-tests", "identity-worker-tests", "db-tests", "contracts-tests",
    "config-worker-tests", "billing-worker-tests", "api-edge-tests", "admin-worker-tests"],
  // run jobs: 13 workers x dev/stage/prod verify-deploy + 2 test verify lanes = 41
  runWorkers: ["policy-worker", "billing-worker", "membership-worker", "events-worker", "metering-worker",
    "config-worker", "webhooks-worker", "projects-worker", "notifications-worker", "identity-worker",
    "integrations-worker", "admin-worker", "api-edge"],
  runTests: ["integrations-worker-tests", "platform-limits-tests"]
};
