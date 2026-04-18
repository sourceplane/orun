const sidebars = {
  docsSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Concepts',
      items: [
        'concepts/intent-model',
        'concepts/compositions',
        'concepts/plan-dag',
        'concepts/execution-model',
        'concepts/change-detection',
      ],
    },
    {
      type: 'category',
      label: 'Getting Started',
      items: ['getting-started/installation', 'getting-started/quick-start'],
    },
    {
      type: 'category',
      label: 'CLI',
      items: [
        'cli/arx',
        'cli/arx-plan',
        'cli/arx-run',
        'cli/arx-validate',
        'cli/arx-debug',
        'cli/arx-compositions',
        'cli/arx-component',
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
        'examples/run-with-docker',
        'examples/use-with-tinx',
      ],
    },
    {
      type: 'category',
      label: 'Architecture',
      items: [
        'architecture/internals',
        'architecture/compiler-pipeline',
        'architecture/execution-runtime',
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
        'contributing/extending-arx',
        'contributing/deploying-docs',
      ],
    },
  ],
};

export default sidebars;