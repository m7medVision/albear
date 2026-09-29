// Build target: which environment (dev or prod) and release version an
// extension build is for. Everything that differs between the dev and prod
// extension (key, name, native host, badge, version) is derived here from
// ALBEAR_ENV and ALBEAR_VERSION, mirroring internal/version in Go:
//
//   - ALBEAR_ENV=dev|prod wins; any other non-empty value is an error.
//   - Otherwise a real release version (ALBEAR_VERSION=v1.4.2) means prod,
//     and no version (or the "dev" placeholder) means dev.
//
// So a plain `pnpm build` is a dev build, and a release build passes
// ALBEAR_VERSION.

export type Environment = 'dev' | 'prod'

// PROD_PUBLIC_KEY pins the prod extension ID (legbdpcjojmfelbcjfelmdelnjcnpllc).
// Base64 DER SubjectPublicKeyInfo; swapping this one constant rotates the prod
// identity. Keep internal/install.ChromeExtensionID in sync.
export const PROD_PUBLIC_KEY =
  'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0rUlbOwcLAW75Zoj9wSrnZM1edjGIIRFcuBzwvpSRD5KjI0I72IJrf4rcnamNOXcJRqILk7gd361HOAPnb6NrlyIWqMSwbm1G9xgDs1ew8HOMJ/8xMwzZxq4jDSVtZgWKn+p7ebWSmcrEcttIBaow2YS+RwnFQjbO7u4DxtZxkyYwAIHeEoveK0ekuH1TApdxdJu8Jz1JMmXIgjHVH+/5p6FaZgEND6UuoDOGhC8XTH2r8ioK9zvvRb8QH98qn8c9VvsoX23RoKxyjs0vkMQFsLeWjxPK6EV6fY9VFGg3vbusyIRa5F9X66InQfCMu89JB8dTfOFoVqbAAxWXnMkVwIDAQAB'

// DEV_PUBLIC_KEY is the public half of the committed extension/keys/dev.pem
// and pins the dev extension ID (ohcnnpelgjehhoejpajdkmeejklpaden). Keep
// internal/install.DevExtensionID in sync.
export const DEV_PUBLIC_KEY =
  'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAxrwVHgx7IFZkANzP47DJEryKioaj2e2TZsfcNngKY8mX0DXaVKYCleAZaji/L0q1vlsIM7XqejL2Je5eEczUmHTXXSTl4krdS4pjg7erKmTqCVh4YgMA7GHUppZDVrY5E6CY8WAV46jRfVsbnj1+QS4ojpi2AmrSveQJ7921IAl8apKszNQ1imbwGzBM4PFERj8jenHCcLWpmRKARS/9U3ce2GlDFYE3P4eB29TY/91YNzM33QOA3vQ4ZjiyZaXVlpz/hvdPlnwS0tLRYCEqamWd/86b+Gzt47fp788PxmYJA6VSn1iDHNoF0y84xK0RPz1gsrc8nxPYvyEOKhojZQIDAQAB'

// DEFAULT_VERSION is the manifest version of a build with no release version.
export const DEFAULT_VERSION = '0.1.0'

export interface BuildTarget {
  environment: Environment
  /** Manifest version: numbers only, e.g. "1.4.2". */
  version: string
  name: string
  /** Base64 DER public key pinned as the manifest "key". */
  key: string
  nativeHost: string
  /** Text on the toolbar icon's badge; empty for none. */
  badge: string
}

// Everything the extension code needs at runtime, injected by vite's define.
export type RuntimeBuild = Pick<BuildTarget, 'environment' | 'nativeHost' | 'badge'>

const SEMVER =
  /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/

// manifestVersion turns a release version into a Chrome manifest version:
// the leading "v" and any prerelease or build suffix go, because Chrome
// versions are dot-separated integers only (v1.4.2-rc.3 -> 1.4.2). Browsers
// only upgrade an external extension when this number increases.
export function manifestVersion(release: string): string {
  const m = SEMVER.exec(release.trim())
  if (!m) throw new Error(`invalid ALBEAR_VERSION ${JSON.stringify(release)} (want e.g. v1.4.2)`)
  const parts = m.slice(1, 4)
  for (const p of parts) {
    if (Number(p) > 65535) throw new Error(`ALBEAR_VERSION ${release}: Chrome limits each part to 65535`)
  }
  return parts.join('.')
}

type Env = Record<string, string | undefined>

export function resolveTarget(env: Env): BuildTarget {
  const rawVersion = env.ALBEAR_VERSION?.trim() ?? ''
  const hasVersion = rawVersion !== '' && rawVersion !== 'dev'
  const version = hasVersion ? manifestVersion(rawVersion) : DEFAULT_VERSION

  let environment: Environment = hasVersion ? 'prod' : 'dev'
  const rawEnv = env.ALBEAR_ENV?.trim() ?? ''
  if (rawEnv !== '') {
    if (rawEnv !== 'dev' && rawEnv !== 'prod') {
      throw new Error(`invalid ALBEAR_ENV ${JSON.stringify(rawEnv)} (want "dev" or "prod")`)
    }
    environment = rawEnv
  }

  if (environment === 'prod') {
    return {
      environment,
      version,
      name: 'albear — البير',
      key: PROD_PUBLIC_KEY,
      nativeHost: 'dev.albear.native',
      badge: '',
    }
  }
  return {
    environment,
    version,
    name: 'albear (dev)',
    key: DEV_PUBLIC_KEY,
    nativeHost: 'dev.albear.native_dev',
    badge: 'DEV',
  }
}

export function runtimeBuild(t: BuildTarget): RuntimeBuild {
  return { environment: t.environment, nativeHost: t.nativeHost, badge: t.badge }
}
