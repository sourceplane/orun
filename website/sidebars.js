const sidebars = {
  docsSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Concepts',
      items: [
        'concepts/intent-model',
        'concepts/compositions',
        'concepts/stacks',
        'concepts/trigger-bindings',
        'concepts/profile-rules',
        'concepts/environment-promotion',
        'concepts/runtime-environment',
        'concepts/plan-dag',
        'concepts/execution-model',
        'concepts/change-detection',
        'concepts/change-watches',
        'concepts/context-discovery',
        'concepts/intent-presets',
      ],
    },
    {
      type: 'category',
      label: 'Getting Started',
      items: ['getting-started/installation', 'getting-started/quick-start'],
    },
    {
      type: 'category',
      label: 'AI Context',
      items: ['ai-context/orun-repositories'],
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
      label: 'Compositions',
      items: [
        'compositions/composition-contract',
        'compositions/writing-compositions',
        'compositions/composition-examples',
      ],
    },
    {
      type: 'category',
      label: 'Examples',
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
      label: 'Architecture',
      items: [
        'architecture/internals',
        'architecture/compiler-pipeline',
        'architecture/execution-runtime',
        'architecture/tui-cockpit',
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
      label: 'Contributing',
      items: [
        'contributing/contributing',
        'contributing/extending-orun',
        'contributing/deploying-docs',
      ],
    },
    {
      type: 'category',
      label: 'Downstream',
      items: ['downstream/v2.6-integration'],
    },
    {
      type: 'category',
      label: 'Release Notes',
      items: ['release-notes/v2.7.0', 'release-notes/v2.6.0'],
    },
  ],
};

export default sidebars;
