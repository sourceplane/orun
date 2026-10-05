/**
 * orun documentation sidebar
 *
 * Shaped around the four pillars of platform discipline as code:
 *
 *   overview  → what orun is, the principles, how it works, the resource model
 *   start     → install, quick start
 *   1 declare → structure and standards as intent: the intent model,
 *               compositions, and the rules that adapt them to events
 *   2 package → standards that travel like code: Stacks, presets,
 *               authoring golden paths, baselines
 *   3 agents  → grounding coding agents in the same intent
 *   4 run     → verify, plan, execute, and the record they leave
 *   platform  → tenancy and workspace discovery, which support all four
 *   cli       → command reference, grouped by pillar
 *   then architecture, reference, build, and release notes
 *
 * Pages are grouped here only; their file paths, and so their URLs, are
 * stable.
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
        'principles',
        'overview/how-orun-works',
        'overview/resource-model',
        'overview/glossary',
      ],
    },
    {
      type: 'category',
      label: 'Start',
      collapsed: false,
      items: ['start/installation', 'start/quick-start'],
    },
    {
      type: 'category',
      label: '1 · Declare',
      items: [
        'concepts/intent-model',
        'concepts/standards',
        'concepts/compositions',
        'compositions/composition-contract',
        'concepts/trigger-bindings',
        'concepts/profile-rules',
        'concepts/dependency-rules',
        'concepts/environment-promotion',
        'concepts/secrets',
        'concepts/runtime-environment',
      ],
    },
    {
      type: 'category',
      label: '2 · Package & evolve',
      items: [
        'concepts/stacks',
        'compositions/writing-compositions',
        'compositions/composition-examples',
        'concepts/intent-presets',
        'concepts/versioning-and-locking',
        'concepts/baselines',
        'examples/bootstrap-a-product-from-a-baseline',
        'examples/use-with-kiox',
      ],
    },
    {
      type: 'category',
      label: '3 · Ground agents',
      items: [
        'ai-context/orun-repositories',
        'concepts/agent-runtime',
        'concepts/task-plane',
      ],
    },
    {
      type: 'category',
      label: '4 · Verify → plan → execute',
      items: [
        'concepts/plan-dag',
        'concepts/change-detection',
        'concepts/change-watches',
        'concepts/execution-model',
        'concepts/workflow-actions',
        'execute/runners',
        'execute/terraform-state',
        {
          type: 'category',
          label: 'The record',
          items: [
            'concepts/state-model',
            'concepts/service-catalog',
            'cockpit/overview',
          ],
        },
        {
          type: 'category',
          label: 'Guides',
          items: [
            'examples/review-pull-request',
            'examples/trigger-bindings-ci',
            'examples/run-github-actions',
            'examples/remote-state-matrix',
            'examples/run-with-docker',
          ],
        },
      ],
    },
    {
      type: 'category',
      label: 'Platform and operations',
      items: [
        'concepts/workspaces-and-tenancy',
        'concepts/context-discovery',
      ],
    },
    {
      type: 'category',
      label: 'CLI',
      items: [
        'cli/orun',
        {
          type: 'category',
          label: '1 · Declare',
          items: ['cli/orun-intent', 'cli/orun-component', 'cli/orun-compositions', 'cli/orun-work'],
        },
        {
          type: 'category',
          label: '2 · Package & evolve',
          items: [
            'cli/orun-new',
            'cli/orun-baseline',
            'cli/orun-pack',
            'cli/orun-publish',
            'cli/orun-fetch',
            'cli/orun-login',
          ],
        },
        {
          type: 'category',
          label: '3 · Ground agents',
          items: [
            'cli/orun-mcp',
            'cli/orun-skills',
            'cli/orun-agent',
            'cli/orun-task',
            'cli/orun-spec',
            'cli/orun-pr',
            'cli/orun-githooks',
          ],
        },
        {
          type: 'category',
          label: '4 · Verify → plan → execute',
          items: [
            'cli/orun-validate',
            'cli/orun-debug',
            'cli/orun-plan',
            'cli/orun-run',
            'cli/orun-workflow',
            'cli/orun-approve',
            'cli/orun-status',
            'cli/orun-logs',
            'cli/orun-describe',
            'cli/orun-get',
            'cli/orun-tui',
            'cli/orun-tui-next',
            'cli/orun-github',
            'cli/orun-catalog',
            'cli/orun-objects',
            'cli/orun-gc',
          ],
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
        'cockpit/architecture',
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
        'release-notes/v2.73.5',
        'release-notes/v2.73.4',
        'release-notes/v2.73.3',
        'release-notes/v2.73.2',
        'release-notes/v2.73.1',
        'release-notes/v2.73.0',
        'release-notes/v2.72.1',
        'release-notes/v2.72.0',
        'release-notes/v2.71.0',
        'release-notes/v2.70.0',
        'release-notes/v2.69.0',
        'release-notes/v2.68.0',
        'release-notes/v2.67.0',
        'release-notes/v2.66.0',
        'release-notes/v2.65.0',
        'release-notes/v2.64.0',
        'release-notes/v2.63.0',
        'release-notes/v2.62.0',
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
