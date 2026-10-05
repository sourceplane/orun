import { createRequire } from 'module';

const require = createRequire(import.meta.url);

const config = {
  title: 'orun',
  tagline: 'Platform discipline as code. Declare your platform\'s structure and standards as intent, package them as baselines, ground your agents in them, and let a runner verify, plan, and execute.',
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
      { name: 'description', content: 'orun is platform discipline as code: an open-source declarative language for your platform\'s structure and standards, packaged as versioned baselines, read by your coding agents, and verified, planned, and executed by a deterministic runner. Orunbase is its hosted control plane.' },
    ],
    navbar: {
      title: 'orun',
      items: [
        { to: '/', label: 'Docs', position: 'left' },
        { to: '/overview/what-is-orun', label: 'Overview', position: 'left' },
        { to: '/concepts/intent-model', label: 'Declare', position: 'left' },
        { to: '/concepts/stacks', label: 'Package', position: 'left' },
        { to: '/ai-context/orun-repositories', label: 'Agents', position: 'left' },
        { to: '/overview/how-orun-works', label: 'Run', position: 'left' },
        { to: '/cli/orun', label: 'CLI', position: 'left' },
        { href: 'https://docs.orunbase.com', label: 'Orunbase', position: 'left' },
        { href: 'https://github.com/sourceplane/orun/releases', label: 'Releases', position: 'right' },
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
            { label: 'Design principles', to: '/principles' },
            { label: 'Installation', to: '/start/installation' },
            { label: 'Quick start', to: '/start/quick-start' },
            { label: 'Glossary', to: '/overview/glossary' },
          ],
        },
        {
          title: 'Declare & package',
          items: [
            { label: 'Intent model', to: '/concepts/intent-model' },
            { label: 'Compositions', to: '/concepts/compositions' },
            { label: 'Stacks', to: '/concepts/stacks' },
            { label: 'Intent presets', to: '/concepts/intent-presets' },
            { label: 'Baselines', to: '/concepts/baselines' },
          ],
        },
        {
          title: 'Agents & runner',
          items: [
            { label: 'Agents in orun repositories', to: '/ai-context/orun-repositories' },
            { label: 'Agent runtime', to: '/concepts/agent-runtime' },
            { label: 'How orun works', to: '/overview/how-orun-works' },
            { label: 'Plan DAG', to: '/concepts/plan-dag' },
            { label: 'Runners', to: '/execute/runners' },
            { label: 'CLI', to: '/cli/orun' },
          ],
        },
        {
          title: 'Build',
          items: [
            { label: 'Architecture', to: '/architecture/internals' },
            { label: 'Contributing', to: '/contributing/' },
            { label: 'Extending orun', to: '/contributing/extending-orun' },
            { label: 'Security policy', href: 'https://github.com/sourceplane/orun/blob/main/SECURITY.md' },
            { label: 'Orunbase docs', href: 'https://docs.orunbase.com' },
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
