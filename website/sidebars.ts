import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/**
 * Curated sidebar for the current docs version (versioned snapshots keep
 * their own sidebar files under versioned_sidebars/).
 *
 * Structure follows the Django/Laravel documentation shape:
 * Getting started → Concepts → Features → Operations → Reference →
 * Architecture, with the FAQ as the closing top-level page.
 */
const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Getting started',
      collapsed: false,
      link: {
        type: 'generated-index',
        title: 'Getting started',
        description: 'Install the CLI, scaffold a project, run the server.',
      },
      items: [
        'getting-started/installation',
        'getting-started/quickstart',
        'getting-started/project-structure',
      ],
    },
    {
      type: 'category',
      label: 'Concepts',
      collapsed: false,
      link: {
        type: 'generated-index',
        title: 'Concepts',
        description:
          'The runtime building blocks: application container, configuration, routing, models.',
      },
      items: [
        'concepts/application',
        'concepts/configuration',
        'concepts/routing',
        'concepts/models-and-database',
      ],
    },
    {
      type: 'category',
      label: 'Features',
      collapsed: false,
      link: {
        type: 'generated-index',
        title: 'Features',
        description:
          'Auth, multi-tenancy, observability, storage, background tasks, and the orbit admin module.',
      },
      items: [
        'features/admin',
        'features/auth',
        'features/observability',
        'features/storage-and-tasks',
      ],
    },
    {
      type: 'category',
      label: 'Operations',
      collapsed: false,
      link: {
        type: 'generated-index',
        title: 'Operations',
        description:
          'Running Nucleus in production: deployment, security hardening, and upgrades.',
      },
      items: [
        'operations/deployment',
        'operations/security',
        'operations/upgrade',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      collapsed: false,
      link: {
        type: 'generated-index',
        title: 'Reference',
        description:
          'The nucleus CLI, every configuration key, and what changed in each release.',
      },
      items: [
        'cli/overview',
        'reference/configuration',
        'reference/release-notes',
      ],
    },
    {
      type: 'category',
      label: 'Architecture',
      collapsed: false,
      link: {
        type: 'generated-index',
        title: 'Architecture',
        description:
          'Principles, contracts and the compatibility policy that pin the public surface.',
      },
      items: ['architecture/principles', 'architecture/compatibility'],
    },
    'faq',
  ],
};

export default sidebars;
