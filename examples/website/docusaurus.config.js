const config = {
  title: 'Example Platform Docs',
  tagline: 'Static docs deploy paths for Cloudflare Pages through Gluon compositions',
  url: 'https://example-platform-docs.pages.dev',
  baseUrl: '/',
  organizationName: 'sourceplane',
  projectName: 'example-platform-repo',
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
      title: 'Example Platform',
      items: [
        {
          to: '/',
          label: 'Docs',
          position: 'left',
        },
        {
          href: 'https://github.com/sourceplane/example-platform-repo',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Paths',
          items: [
            { label: 'Wrangler Direct Upload', to: '/deployment-paths#wrangler-direct-upload' },
            { label: 'Terraform Git Source', to: '/deployment-paths#terraform-git-source' },
          ],
        },
        {
          title: 'Repo',
          items: [
            { label: 'Intent', href: 'https://github.com/sourceplane/example-platform-repo/blob/main/intent.yaml' },
            { label: 'Compositions', href: 'https://github.com/sourceplane/example-platform-repo/tree/main/compositions' },
          ],
        },
      ],
      copyright: `Copyright ${new Date().getFullYear()} sourceplane`,
    },
    prism: {
      additionalLanguages: ['bash', 'json', 'yaml'],
    },
  },
};

module.exports = config;
