# Nucleus documentation site

Public documentation site for [Nucleus](https://github.com/jcsvwinston/nucleus),
served on GitHub Pages at <https://jcsvwinston.github.io/nucleus/>.

Built with [Docusaurus 3](https://docusaurus.io/) (Classic preset, TypeScript).
The authoritative content remains under the repository's `docs/` tree and is
promoted into this site incrementally — see
[`.claude/state/CURRENT_ITERATION.md`](../.claude/state/CURRENT_ITERATION.md)
for the active scope.

## Local development

Requires Node.js 20+.

```bash
cd website
npm ci
npm start          # dev server with hot reload
npm run build      # production build into website/build
npm run serve      # serve the production build locally
```

## Deployment

Deployment is automated by `.github/workflows/docs.yml`:

- Pull requests run a build-only check.
- Pushes to `main` build and publish to GitHub Pages via the official
  `actions/deploy-pages` flow.

The legacy `npm run deploy` (gh-pages branch flow) is **not** used. Do not run
it manually — it would compete with the Actions-based deployment.

## Configuration pointers

- Site config: [`docusaurus.config.ts`](./docusaurus.config.ts).
- Sidebar: [`sidebars.ts`](./sidebars.ts).
- Doc pages: [`docs/`](./docs/).
- Static assets: [`static/`](./static/).
