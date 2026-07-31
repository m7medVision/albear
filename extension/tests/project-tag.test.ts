// Project-tag extraction (dev-mode localhost credentials): pure DOM logic,
// unit-testable in isolation from message passing or matching.
// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import { readProjectTag } from '../src/content/project-tag'

beforeEach(() => {
  document.body.innerHTML = ''
  for (const name of document.body.getAttributeNames()) {
    document.body.removeAttribute(name)
  }
})

describe('readProjectTag', () => {
  it('reads the tag from any element carrying albear-id', () => {
    document.body.innerHTML = `<div albear-id="my-app"></div>`
    expect(readProjectTag(document)).toBe('my-app')
  })

  it('reads the tag from the body tag itself', () => {
    document.body.setAttribute('albear-id', 'my-app')
    expect(readProjectTag(document)).toBe('my-app')
  })

  it('trims surrounding whitespace', () => {
    document.body.innerHTML = `<form albear-id="  my-app  "></form>`
    expect(readProjectTag(document)).toBe('my-app')
  })

  it('returns null when no element carries the attribute', () => {
    document.body.innerHTML = `<div id="my-app"></div>`
    expect(readProjectTag(document)).toBeNull()
  })

  it('returns null for an empty or whitespace-only attribute value', () => {
    document.body.innerHTML = `<div albear-id="   "></div>`
    expect(readProjectTag(document)).toBeNull()
  })

  it('uses the first matching element in document order when more than one is present', () => {
    document.body.innerHTML = `<div albear-id="first"></div><div albear-id="second"></div>`
    expect(readProjectTag(document)).toBe('first')
  })
})
