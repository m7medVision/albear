// Build target and generated manifest per environment: the dev and prod
// extensions must differ in key (ID), name, native host and badge, and the
// manifest version is the release version without its prerelease suffix.
import { describe, expect, it, vi } from 'vitest'
import { createHash, createPublicKey } from 'node:crypto'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import {
  DEFAULT_VERSION,
  DEV_PUBLIC_KEY,
  PROD_PUBLIC_KEY,
  manifestVersion,
  resolveTarget,
  runtimeBuild,
} from '../build/target'
import { createManifest } from '../build/manifest'
import { showEnvironmentBadge } from '../src/background/badge'
import { BUILD } from '../src/build-info'
import { NATIVE_HOST } from '../src/messaging/transport'

// Chrome's extension ID: first 16 bytes of SHA-256 over the DER public key,
// hex digits 0-f mapped to a-p.
function extensionID(base64Key: string): string {
  const hex = createHash('sha256').update(Buffer.from(base64Key, 'base64')).digest('hex').slice(0, 32)
  return [...hex].map((c) => String.fromCharCode('a'.charCodeAt(0) + parseInt(c, 16))).join('')
}

describe('manifestVersion', () => {
  it.each([
    ['v1.4.2', '1.4.2'],
    ['1.4.2', '1.4.2'],
    ['v1.4.2-rc.3', '1.4.2'],
    ['v0.10.0-beta+build.7', '0.10.0'],
    ['v2.0.0+meta', '2.0.0'],
  ])('%s -> %s', (release, want) => {
    expect(manifestVersion(release)).toBe(want)
  })

  it.each(['dev', 'v1.2', 'v1.2.3.4', 'latest', 'v01.2.3', 'v1.65536.0'])('rejects %s', (bad) => {
    expect(() => manifestVersion(bad)).toThrow()
  })
})

describe('resolveTarget', () => {
  it('builds dev with neither environment nor version', () => {
    expect(resolveTarget({})).toEqual({
      environment: 'dev',
      version: DEFAULT_VERSION,
      name: 'albear (dev)',
      key: DEV_PUBLIC_KEY,
      nativeHost: 'dev.albear.native_dev',
      badge: 'DEV',
    })
    expect(resolveTarget({ ALBEAR_VERSION: 'dev', ALBEAR_ENV: '' }).environment).toBe('dev')
  })

  it('builds prod for a release version', () => {
    expect(resolveTarget({ ALBEAR_VERSION: 'v1.4.2-rc.3' })).toEqual({
      environment: 'prod',
      version: '1.4.2',
      name: 'albear — البير',
      key: PROD_PUBLIC_KEY,
      nativeHost: 'dev.albear.native',
      badge: '',
    })
  })

  it('lets ALBEAR_ENV override the environment', () => {
    const dev = resolveTarget({ ALBEAR_ENV: 'dev', ALBEAR_VERSION: 'v2.0.0' })
    expect([dev.environment, dev.version, dev.nativeHost]).toEqual(['dev', '2.0.0', 'dev.albear.native_dev'])
    const prod = resolveTarget({ ALBEAR_ENV: 'prod' })
    expect([prod.environment, prod.version, prod.key]).toEqual(['prod', DEFAULT_VERSION, PROD_PUBLIC_KEY])
  })

  it('rejects an invalid environment or version', () => {
    expect(() => resolveTarget({ ALBEAR_ENV: 'staging' })).toThrow(/ALBEAR_ENV/)
    expect(() => resolveTarget({ ALBEAR_VERSION: 'nightly' })).toThrow(/ALBEAR_VERSION/)
  })
})

describe('createManifest', () => {
  it.each([
    ['dev', {}, 'ohcnnpelgjehhoejpajdkmeejklpaden', 'albear (dev)', 'albear (dev)', DEFAULT_VERSION],
    ['prod', { ALBEAR_VERSION: 'v1.4.2-rc.3' }, 'iblbbooeonkneacnoakpkkpdpehdhdna', 'albear — البير', 'albear', '1.4.2'],
  ] as const)('%s', (_env, vars, id, name, title, version) => {
    const m = createManifest(resolveTarget(vars))
    expect(extensionID(m.key!)).toBe(id)
    expect(m.name).toBe(name)
    expect(m.action?.default_title).toBe(title)
    expect(m.version).toBe(version)
    expect(m.permissions).toEqual(['nativeMessaging', 'activeTab', 'storage', 'scripting'])
  })
})

describe('dev key', () => {
  it('DEV_PUBLIC_KEY is the public half of the committed keys/dev.pem', () => {
    const pem = readFileSync(path.join(__dirname, '..', 'keys', 'dev.pem'))
    const der = createPublicKey(pem).export({ type: 'spki', format: 'der' })
    expect(der.toString('base64')).toBe(DEV_PUBLIC_KEY)
  })
})

describe('runtime build', () => {
  it('injects the target vite resolved into the bundle', () => {
    const target = resolveTarget(process.env)
    expect(BUILD).toEqual(runtimeBuild(target))
    expect(NATIVE_HOST).toBe(target.nativeHost)
  })

  it('shows the badge only when the build has one', () => {
    const action = { setBadgeText: vi.fn(), setBadgeBackgroundColor: vi.fn() }
    showEnvironmentBadge(action as never, '')
    expect(action.setBadgeText).not.toHaveBeenCalled()
    showEnvironmentBadge(action as never, 'DEV')
    expect(action.setBadgeText).toHaveBeenCalledWith({ text: 'DEV' })
  })
})
