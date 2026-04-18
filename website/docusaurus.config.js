import { createRequire } from 'module';

const require = createRequire(import.meta.url);

const config = {
  title: 'arx',
  tagline: 'Policy-aware planner that compiles intent into deterministic execution DAGs',
  url: 'https://arx-docs.pages.dev',
  baseUrl: '/',
  organizationName: 'sourceplane',
  projectName: 'arx',
  onBrokenLinks: 'throw',
  onDuplicateRoutes: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },
  presets: [
    [
      'classic',
      {
        docs: {
          path: 'docs',
          routeBasePath: '/',
          sidebarPath: require.resolve('./sidebars.js'),
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      },
    ],
  ],
  themeConfig: {
    colorMode: {
      defaultMode: 'light',
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'arx',
      items: [
        {
          to: '/',
          label: 'Documentation',
          position: 'left',
        },
        {
          href: 'https://github.com/sourceplane/arx',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Getting Started',
          items: [
            { label: 'Installation', to: '/getting-started/installation' },
            { label: 'Quick Start', to: '/getting-started/quick-start' },
          ],
        },
        {
          title: 'Core Concepts',
          items: [
            { label: 'Intent Model', to: '/concepts/intent-model' },
            { label: 'Compositions', to: '/concepts/compositions' },
            { label: 'Execution Model', to: '/concepts/execution-model' },
          ],
        },
        {
          title: 'Reference',
          items: [
            { label: 'CLI Overview', to: '/cli/arx' },
            { label: 'Configuration', to: '/reference/configuration' },
            { label: 'Deploy Docs', to: '/contributing/deploying-docs' },
          ],
        },
      ],
      copyright: `Copyright ${new Date().getFullYear()} sourceplane`,
    },
    prism: {
      additionalLanguages: ['bash', 'go', 'json', 'yaml'],
    },
  },
};

export default config;