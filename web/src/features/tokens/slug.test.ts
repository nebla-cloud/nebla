import { describe, expect, it } from 'vitest'
import { tokenSlug } from './slug'

describe('tokenSlug', () => {
  it('lowercases and dashes runs of other characters', () => {
    expect(tokenSlug('My Homelab!')).toBe('my-homelab')
    expect(tokenSlug('Home lab')).toBe('home-lab')
  })
  it('falls back to project', () => {
    expect(tokenSlug('!!!')).toBe('project')
  })
})
