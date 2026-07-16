# Albear Desktop

Desktop app for Albear, the local-only encrypted secrets manager. Built on
Electron + React + webpack (based on electron-react-boilerplate).

The UI mirrors the Albear Chrome extension: the design tokens in
`src/renderer/styles/globals.css` and the components in
`src/renderer/components/ui/` are copied from `extension/src/` and styled
with Tailwind CSS v4 (via `@tailwindcss/postcss` in the webpack pipeline).

## Development

```bash
npm install
npm start          # dev server + electron with hot reload
```

## Build and package

```bash
npm run build      # production webpack build (main + renderer)
npm run package    # build platform installers into release/build/
npm run lint
npm test
```

## Versioning and updates

The app version lives in `release/app/package.json` and is kept in lockstep
with the repository-wide `vX.Y.Z` git tag that releases all Albear
components. Auto-updates use `electron-updater` against GitHub releases;
the publish target (owner/repo) is configured once in the root
`package.json` under `build.publish`. Update checks are skipped entirely in
unpackaged (development) builds.
