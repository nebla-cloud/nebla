import { describe, expect, it } from 'vitest'
import { osLabel, osParts } from './osLabel'

const img = (os_flavor: string, os_version: string, name = 'slug-1') => ({
  name,
  os_flavor,
  os_version,
})

describe('osLabel', () => {
  it.each([
    ['debian', '13', 'Debian 13'],
    ['ubuntu', '24.04', 'Ubuntu 24.04'],
    ['rocky', '9', 'Rocky Linux 9'],
    ['alma', '9.4', 'AlmaLinux 9.4'],
    ['fedora', '41', 'Fedora 41'],
  ])('%s %s reads %s', (flavor, version, want) => {
    expect(osLabel(img(flavor, version))).toBe(want)
  })

  it('ignores case and stray spaces in the flavor', () => {
    expect(osLabel(img(' Debian ', '13'))).toBe('Debian 13')
  })

  it('falls back to the image name for an unknown flavor', () => {
    expect(osLabel(img('arch', 'rolling', 'arch-linux'))).toBe('arch-linux')
  })

  it('drops an empty version', () => {
    expect(osLabel(img('debian', ''))).toBe('Debian')
  })
})

describe('osParts', () => {
  it('splits a known flavor into name and version', () => {
    expect(osParts(img('ubuntu', '24.04'))).toEqual({ name: 'Ubuntu', version: '24.04' })
  })

  it('is null for an unknown flavor', () => {
    expect(osParts(img('nixos', '24.11'))).toBeNull()
  })
})
