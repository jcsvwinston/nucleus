import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js — don't use client-side code here (browser APIs, JSX…).

const config: Config = {
  title: 'Nucleus',
  tagline: 'Stdlib-first MVC + REST framework for Go',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  url: 'https://jcsvwinston.github.io',
  baseUrl: '/nucleus/',

  organizationName: 'jcsvwinston',
  projectName: 'nucleus',
  trailingSlash: false,

  onBrokenLinks: 'throw',

  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
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
          sidebarPath: './sidebars.ts',
          editUrl:
            'https://github.com/jcsvwinston/nucleus/tree/main/website/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Nucleus',
      logo: {
        alt: 'Nucleus logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'tutorialSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          to: '/docs/getting-started/quickstart',
          label: 'Quickstart',
          position: 'left',
        },
        {
          to: '/docs/architecture/principles',
          label: 'Architecture',
          position: 'left',
        },
        {
          href: 'https://github.com/jcsvwinston/nucleus',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Introduction', to: '/docs/'},
            {label: 'Quickstart', to: '/docs/getting-started/quickstart'},
            {label: 'Concepts', to: '/docs/concepts/application'},
            {label: 'Architecture', to: '/docs/architecture/principles'},
            {label: 'CLI', to: '/docs/cli/overview'},
          ],
        },
        {
          title: 'Project',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/jcsvwinston/nucleus',
            },
            {
              label: 'Issues',
              href: 'https://github.com/jcsvwinston/nucleus/issues',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Nucleus contributors. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'go', 'yaml', 'json', 'toml'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
