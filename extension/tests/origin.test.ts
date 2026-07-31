import { describe, expect, it } from 'vitest'
import { isLoopbackHost } from '../src/lib/origin'

describe('isLoopbackHost', () => {
  it('accepts localhost and loopback IPv4/IPv6 literals', () => {
    for (const host of ['localhost', 'LOCALHOST', '127.0.0.1', '127.5.9.1', '::1']) {
      expect(isLoopbackHost(host)).toBe(true)
    }
  })

  it('rejects real hostnames and private LAN IPs', () => {
    for (const host of ['example.com', '192.168.1.5', '10.0.0.1', 'localhost.example.com', 'notlocalhost']) {
      expect(isLoopbackHost(host)).toBe(false)
    }
  })
})
