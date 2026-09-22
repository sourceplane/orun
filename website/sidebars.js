/**
 * orun documentation sidebar
 *
 * Shaped around the operator journey:
 *
 *   1. overview      → what orun is, how it works, the resource model,
 *                      design principles, glossary
 *   2. start         → install, quick start
 *   3. concepts      → the model: intent, compositions, plans, execution,
 *                      catalog, state, secrets, tenancy, baselines, tasks, agents
 *   4. cockpit       → the operator surface
 *   5. execute       → runners, terraform state
 *   6. guides        → end-to-end walkthroughs
 *   7. cli           → command reference, one page per command
 *   8. compositions  → authoring guide
 *   9. architecture  → internals
 *  10. reference     → schemas, configuration, environment variables
 *  11. ai-context    → for coding agents working with orun repos
 *  12. build         → contributing, extending, deploying docs
 *  13. release-notes
 */
const sidebars = {
  docsSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Overview',
      collapsed: false,
      items: [
        'overview/what-is-orun',
        'overview/how-orun-works',
        'overview/resource-model',
        'principles',
        'overview/glossary',
      ],
    },
    {
      type: 'category',
      label: 'Start',
      collapsed: false,
      items: [
        'start/installation',
        'start/quick-start',
        'examples/bootstrap-a-product-from-a-baseline',
      ],
    },
    {
      type: 'category',
      label: 'Concepts',
      items: [
        'concepts/intent-model',
        'concepts/compositions',
        'concepts/stacks',
        'concepts/plan-dag',
        'concepts/execution-model',
        'concepts/workflow-actions',
        'concepts/trigger-bindings',
        'concepts/profile-rules',
        'concepts/dependency-rules',
        'concepts/environment-promotion',
        'concepts/runtime-environment',
        'concepts/service-catalog',
        'concepts/state-model',
        'concepts/secrets',
        'concepts/change-detection',
        'concepts/change-watches',
        'concepts/context-discovery',
        'concepts/intent-presets',
        'concepts/workspaces-and-tenancy',
        'concepts/baselines',
        'concepts/task-plane',
        'concepts/agent-runtime',
      ],
    },
    {
      type: 'category',
      label: 'Cockpit',
      items: [
        'cockpit/overview',
        'cockpit/architecture',
      ],
    },
    {
      type: 'category',
      label: 'Execute',
      items: [
        'execute/runners',
        'execute/terraform-state',
      ],
    },
    {
      type: 'category',
      label: 'Guides',
      items: [
        'examples/bootstrap-a-product-from-a-baseline',
        'examples/review-pull-request',
        'examples/run-github-actions',
        'examples/trigger-bindings-ci',
        'examples/remote-state-matrix',
        'examples/run-with-docker',
        'examples/use-with-kiox',
      ],
    },
    {
      type: 'category',
      label: 'CLI',
      items: [
        'cli/orun',
        {
          type: 'category',
          label: 'Compile and inspect',
          items: [
            'cli/orun-plan',
            'cli/orun-validate',
            'cli/orun-debug',
            'cli/orun-intent',
            'cli/orun-component',
            'cli/orun-compositions',
            'cli/orun-describe',
            'cli/orun-get',
          ],
        },
        {
          type: 'category',
          label: 'Run and operate',
          items: [
            'cli/orun-run',
            'cli/orun-workflow',
            'cli/orun-approve',
            'cli/orun-status',
            'cli/orun-logs',
            'cli/orun-tui',
            'cli/orun-tui-next',
            'cli/orun-github',
            'cli/orun-gc',
          ],
        },
        {
          type: 'category',
          label: 'Catalog and objects',
          items: ['cli/orun-catalog', 'cli/orun-objects'],
        },
        {
          type: 'category',
          label: 'Scaffolding and baselines',
          items: ['cli/orun-new', 'cli/orun-baseline'],
        },
        {
          type: 'category',
          label: 'Composition packaging',
          items: ['cli/orun-pack', 'cli/orun-publish', 'cli/orun-fetch', 'cli/orun-login'],
        },
        {
          type: 'category',
          label: 'Cloud client',
          items: [
            'cli/orun-auth',
            'cli/orun-workspace',
            'cli/orun-cloud',
            'cli/orun-secrets',
            'cli/orun-integrations',
            'cli/orun-policy',
            'cli/orun-backend',
          ],
        },
        {
          type: 'category',
          label: 'Tasks and provenance',
          items: ['cli/orun-task', 'cli/orun-pr', 'cli/orun-githooks', 'cli/orun-spec'],
        },
        {
          type: 'category',
          label: 'Agents',
          items: ['cli/orun-agent', 'cli/orun-mcp', 'cli/orun-skills'],
        },
      ],
    },
    {
      type: 'category',
      label: 'Authoring compositions',
      items: [
        'compositions/composition-contract',
        'compositions/writing-compositions',
        'compositions/composition-examples',
      ],
    },
    {
      type: 'category',
      label: 'Architecture',
      items: [
        'architecture/internals',
        'architecture/compiler-pipeline',
        'architecture/execution-runtime',
        'architecture/github-artifacts',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'reference/configuration',
        'reference/plan-schema',
        'reference/workflow-schema',
        'reference/scope-references',
        'reference/environment-variables',
      ],
    },
    {
      type: 'category',
      label: 'AI context',
      items: ['ai-context/orun-repositories'],
    },
    {
      type: 'category',
      label: 'Build',
      items: [
        'contributing/contributing',
        'contributing/extending-orun',
        'contributing/deploying-docs',
      ],
    },
    {
      type: 'category',
      label: 'Release notes',
      items: [
        'release-notes/v2.61.0',
        'release-notes/v2.60.0',
        'release-notes/v2.59.0',
        'release-notes/v2.58.0',
        'release-notes/v2.54.0',
        'release-notes/v2.52.0',
        'release-notes/v2.35.0',
        'release-notes/v2.34.0',
        'release-notes/v2.32.0',
        'release-notes/v2.26.0',
        'release-notes/v2.25.0',
        'release-notes/v2.24.0',
        'release-notes/v2.22.0',
        'release-notes/v2.20.0',
        'release-notes/v2.19.0',
        'release-notes/v2.18.0',
        'release-notes/v2.17.0',
        'release-notes/v2.16.0',
        'release-notes/v2.15.0',
        'release-notes/v2.14.0',
        'release-notes/v2.13.0',
        'release-notes/v2.10.0',
        'release-notes/v2.9.0',
        'release-notes/v2.8.0',
        'release-notes/v2.7.0',
        'release-notes/v2.6.0',
      ],
    },
  ],
};

export default sidebars;
