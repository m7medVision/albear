import { describe, expect, it } from 'vitest'
import { describeError, errorCode } from '../src/popup/errors'

describe('popup error copy', () => {
  it('never shows a raw code and always names a next step', () => {
    for (const code of ['DISCONNECTED', 'AUTH_FAILED', 'RATE_LIMITED', 'VAULT_LOCKED', 'INTERNAL', 'SOMETHING_NEW']) {
      const text = describeError(new Error(code))
      expect(text).not.toContain(code)
      expect(text).toMatch(/try again|continue|choose|pair it again|run/i)
    }
  })

  it('only calls a failed unlock a wrong password when vaultd says so', () => {
    expect(describeError(new Error('AUTH_FAILED'))).toMatch(/wrong master password/i)
    expect(describeError(new Error('DISCONNECTED'))).not.toMatch(/password/i)
  })

  it('reads codes out of stringified errors from the content script', () => {
    expect(errorCode('Error: VAULT_LOCKED')).toBe('VAULT_LOCKED')
  })
})
