import { createRequire } from 'module';

const require = createRequire(import.meta.url);

const config = {
  title: 'orun',
  tagline: 'The planner–cockpit for platform engineering. Plan once, run anywhere, operate from one cockpit.',
  url: 'https://orun-docs.pages.dev',
  baseUrl: '/',
  organizationName: 'sourceplane',
  projectName: 'orun',
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
    metadata: [
      { name: 'theme-color', content: '#f59e0b' },
      { name: 'description', content: 'orun is the planner–cockpit for platform engineering. Compile declarative intent into a deterministic execution DAG, then operate it through a unified CLI + TUI cockpit.' },
    ],
    navbar: {
      title: 'orun',
      items: [
        { to: '/', label: 'Docs', position: 'left' },
        { to: '/overview/what-is-orun', label: 'Overview', position: 'left' },
        { to: '/principles', label: 'Principles', position: 'left' },
        { to: '/cockpit/overview', label: 'Cockpit', position: 'left' },
        { to: '/cli/orun', label: 'CLI', position: 'left' },
        { href: 'https://docs.orun.dev', label: 'Orun Cloud', position: 'left' },
        { to: '/release-notes/v2.22.0', label: 'Releases', position: 'right' },
        {
          href: 'https://github.com/sourceplane/orun',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Start',
          items: [
            { label: 'What is orun?', to: '/overview/what-is-orun' },
            { label: 'Installation', to: '/start/installation' },
            { label: 'Quick start', to: '/start/quick-start' },
            { label: 'Design principles', to: '/principles' },
          ],
        },
        {
          title: 'Model',
          items: [
            { label: 'The resource model', to: '/overview/resource-model' },
            { label: 'Intent model', to: '/concepts/intent-model' },
            { label: 'Compositions', to: '/concepts/compositions' },
            { label: 'Plan DAG', to: '/concepts/plan-dag' },
            { label: 'Service catalog', to: '/concepts/service-catalog' },
            { label: 'Glossary', to: '/overview/glossary' },
          ],
        },
        {
          title: 'Operate',
          items: [
            { label: 'Cockpit overview', to: '/cockpit/overview' },
            { label: 'Runners', to: '/execute/runners' },
            { label: 'CLI', to: '/cli/orun' },
            { label: 'Reference', to: '/reference/configuration' },
            { label: 'Orun Cloud docs', href: 'https://docs.orun.dev' },
          ],
        },
        {
          title: 'Build',
          items: [
            { label: 'Architecture', to: '/architecture/internals' },
            { label: 'Contributing', to: '/contributing/' },
            { label: 'Extending orun', to: '/contributing/extending-orun' },
            { label: 'GitHub', href: 'https://github.com/sourceplane/orun' },
          ],
        },
      ],
      copyright: `▲ orun · MIT licensed · © ${new Date().getFullYear()} sourceplane contributors`,
    },
    prism: {
      additionalLanguages: ['bash', 'go', 'json', 'yaml'],
    },
  },
};

export default config;
