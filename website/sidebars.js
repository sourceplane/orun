/**
 * orun documentation sidebar
 *
 * IA shaped around the operator journey:
 *
 *   1. start      → install, quick-start
 *   2. principles → design ethos (load-bearing)
 *   3. concepts   → the model (intent, compositions, plans, triggers)
 *   4. cockpit    → the operator surface (overview, architecture)
 *   5. execute    → runners, execution model
 *   6. cli        → command reference
 *   7. workflows  → real end-to-end examples
 *   8. compositions → authoring guide
 *   9. architecture → internals (compiler, runtime, artifacts)
 *  10. reference   → schemas, env vars, configuration
 *  11. ai-context  → for coding agents working with orun repos
 *  12. build       → contributing, extending, deploying docs
 *  13. release-notes
 *  14. downstream  → migration / integration notes
 */
const sidebars = {
  docsSidebar: [
    'intro',
    'principles',
    {
      type: 'category',
      label: 'Start',
      collapsed: false,
      items: [
        'start/installation',
        'start/quick-start',
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
        'concepts/trigger-bindings',
        'concepts/profile-rules',
        'concepts/dependency-rules',
        'concepts/environment-promotion',
        'concepts/runtime-environment',
        'concepts/state-model',
        'concepts/change-detection',
        'concepts/change-watches',
        'concepts/context-discovery',
        'concepts/intent-presets',
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
      ],
    },
    {
      type: 'category',
      label: 'CLI',
      items: [
        'cli/orun',
        'cli/orun-plan',
        'cli/orun-run',
        'cli/orun-status',
        'cli/orun-logs',
        'cli/orun-get',
        'cli/orun-describe',
        'cli/orun-state',
        'cli/orun-gc',
        'cli/orun-github',
        'cli/orun-tui',
        'cli/orun-validate',
        'cli/orun-debug',
        'cli/orun-compositions',
        'cli/orun-component',
        'cli/orun-backend',
      ],
    },
    {
      type: 'category',
      label: 'Workflows',
      items: [
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
      items: ['release-notes/v2.10.0', 'release-notes/v2.9.0', 'release-notes/v2.8.0', 'release-notes/v2.7.0', 'release-notes/v2.6.0'],
    },
    {
      type: 'category',
      label: 'Downstream',
      items: ['downstream/v2.6-integration'],
    },
  ],
};

export default sidebars;
