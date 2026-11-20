import { describe, expect, it } from 'vitest'
import { addrFamily, inCidr, rangeSize } from './cidr'

describe('inCidr', () => {
  it('matches ipv4 inside and outside', () => {
    expect(inCidr('203.0.113.1', '203.0.113.0/24')).toBe(true)
    expect(inCidr('203.0.114.1', '203.0.113.0/24')).toBe(false)
    expect(inCidr('10.0.0.5', '10.0.0.4/30')).toBe(true)
    expect(inCidr('10.0.0.8', '10.0.0.4/30')).toBe(false)
  })
  it('matches ipv6 inside and outside', () => {
    expect(inCidr('2001:db8::1', '2001:db8::/32')).toBe(true)
    expect(inCidr('2001:db9::1', '2001:db8::/32')).toBe(false)
    expect(inCidr('2001:db8:0:1::5', '2001:db8:0:1::/64')).toBe(true)
  })
  it('is false for malformed input or mixed families', () => {
    expect(inCidr('nope', '203.0.113.0/24')).toBe(false)
    expect(inCidr('203.0.113.1', '203.0.113.0')).toBe(false)
    expect(inCidr('203.0.113.1', '203.0.113.0/33')).toBe(false)
    expect(inCidr('300.0.113.1', '203.0.113.0/24')).toBe(false)
    expect(inCidr('2001:db8::1', '203.0.113.0/24')).toBe(false)
    expect(inCidr('2001:::1', '2001:db8::/32')).toBe(false)
  })
})

describe('rangeSize', () => {
  it('counts inclusively', () => {
    expect(rangeSize('203.0.113.10', '203.0.113.200')).toBe(191n)
    expect(rangeSize('10.0.0.1', '10.0.0.1')).toBe(1n)
    expect(rangeSize('2001:db8::1', '2001:db8::ff')).toBe(255n)
  })
  it('is 0 for malformed, reversed or mixed input', () => {
    expect(rangeSize('x', '10.0.0.1')).toBe(0n)
    expect(rangeSize('10.0.0.5', '10.0.0.1')).toBe(0n)
    expect(rangeSize('10.0.0.1', '2001:db8::1')).toBe(0n)
  })
})

describe('addrFamily', () => {
  it('detects the family', () => {
    expect(addrFamily('1.2.3.4')).toBe('ipv4')
    expect(addrFamily('::1')).toBe('ipv6')
    expect(addrFamily('zzz')).toBeNull()
  })
})
