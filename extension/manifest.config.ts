import { defineManifest } from '@crxjs/vite-plugin'

const CHROME_EXTENSION_KEY =
  'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAq0MNoBDQinfrDN3gTrGC5IdxEs0tL+9rIE41zuMxLSNiad5zORZageq0r71SnE9hLHF6rDwRO+AJK0/Z+unaCGruQCyaS5wFiQm/RNTXxnBSxEcd1VjBdXUTjAtAlUtXqGIOkb564i4KRILKbW6FyOIvb92XUs6EXkuyUs6u5JbaixYGCPOhAiu4mW2qFgSPL6cIdVSzmzqoyjGo5YqSEmFkfSpPXeDrH29SVNnPAXFo+ZS1eOzh7IyXzkMIOxLDhZ+XfAylyKNqLuaXoTPgqveudwHFqsH2z67Hl3yHkHzqIaBdvcrMy/uRIDfNWM4MzBcIljc8aKazdh5spwkDIwIDAQAB'

// Chrome MV3 manifest (PRD 13.1): least privilege, no remote code, native
// messaging only.
export default defineManifest({
  manifest_version: 3,
  key: CHROME_EXTENSION_KEY,
  name: 'albear — البير',
  version: '0.1.0',
  description: 'Local-only encrypted secrets manager. Talks to vaultd end-to-end encrypted.',
  permissions: ['nativeMessaging', 'activeTab', 'storage', 'scripting'],
  host_permissions: [],
  background: {
    service_worker: 'src/background/index.ts',
    type: 'module',
  },
  action: {
    default_popup: 'src/popup/index.html',
    default_title: 'albear',
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
})
