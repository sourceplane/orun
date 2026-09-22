import { createRequire } from 'module';

const require = createRequire(import.meta.url);

const config = {
  title: 'orun',
  tagline: 'The intent compiler for platform engineering. Write your platform as intent, compile it into one deterministic state, converge the deviation on every commit.',
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
      { name: 'theme-color', content: '#7c3aed' },
      { name: 'description', content: 'orun is an open-source intent compiler for platform engineering: it compiles declarative platform, component, and golden-path intent into a deterministic plan, converges it on every commit, and operates it from one cockpit. Orunbase is its hosted control plane.' },
    ],
    navbar: {
      title: 'orun',
      items: [
        { to: '/', label: 'Docs', position: 'left' },
        { to: '/overview/what-is-orun', label: 'Overview', position: 'left' },
        { to: '/principles', label: 'Principles', position: 'left' },
        { to: '/cockpit/overview', label: 'Cockpit', position: 'left' },
        { to: '/cli/orun', label: 'CLI', position: 'left' },
        { to: '/examples/bootstrap-a-product-from-a-baseline', label: 'Baselines', position: 'left' },
        { href: 'https://docs.orunbase.com', label: 'Orunbase', position: 'left' },
        { to: '/release-notes/v2.58.0', label: 'Releases', position: 'right' },
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
            { label: 'Baselines guide', to: '/examples/bootstrap-a-product-from-a-baseline' },
            { label: 'Orunbase docs', href: 'https://docs.orunbase.com' },
          ],
        },
        {
          title: 'Build',
          items: [
            { label: 'Architecture', to: '/architecture/internals' },
            { label: 'Contributing', to: '/contributing/' },
            { label: 'Security policy', href: 'https://github.com/sourceplane/orun/blob/main/SECURITY.md' },
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
