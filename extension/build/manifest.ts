import type { ManifestV3Export } from '@crxjs/vite-plugin'
import type { BuildTarget } from './target'

// crxjs does not export its plain manifest type; peel it out of the export union.
type Manifest = Exclude<ManifestV3Export, Promise<unknown> | ((...args: never[]) => unknown)>

const ICONS = {
  16: 'icons/16.png',
  32: 'icons/32.png',
  48: 'icons/48.png',
  128: 'icons/128.png',
}

// Chrome MV3 manifest (PRD 13.1): least privilege, no remote code, native
// messaging only. The key, name and version come from the build target, so
// the dev and prod extensions have different IDs and can be loaded side by
// side in one browser profile.
export function createManifest(target: BuildTarget): Manifest {
  return {
    manifest_version: 3,
    key: target.key,
    name: target.name,
    version: target.version,
    description: 'Local-only encrypted secrets manager. Talks to vaultd end-to-end encrypted.',
    permissions: ['nativeMessaging', 'activeTab', 'storage', 'scripting'],
    host_permissions: [],
    // Served from public/, so crxjs copies them to the bundle root.
    icons: ICONS,
    background: {
      service_worker: 'src/background/index.ts',
      type: 'module',
    },
    action: {
      default_popup: 'src/popup/index.html',
      default_title: target.environment === 'dev' ? 'albear (dev)' : 'albear',
      default_icon: ICONS,
    },
    content_scripts: [
      {
        matches: ['https://*/*', 'http://*/*'],
        js: ['src/content/index.ts'],
        run_at: 'document_idle',
      },
    ],
    content_security_policy: {
      extension_pages: "script-src 'self'; object-src 'self'",
    },
  }
}
