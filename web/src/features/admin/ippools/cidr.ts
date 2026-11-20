interface Addr {
  bits: 32 | 128
  value: bigint
}

function parseV4(s: string): bigint | null {
  const parts = s.split('.')
  if (parts.length !== 4) return null
  let v = 0n
  for (const p of parts) {
    if (!/^\d{1,3}$/.test(p) || Number(p) > 255) return null
    v = (v << 8n) | BigInt(p)
  }
  return v
}

function parseV6(s: string): bigint | null {
  if (!/^[0-9a-fA-F:]+$/.test(s) || s.includes(':::')) return null
  const halves = s.split('::')
  if (halves.length > 2) return null
  const groups = (h: string) => (h === '' ? [] : h.split(':'))
  const head = groups(halves[0]!)
  const tail = halves.length === 2 ? groups(halves[1]!) : []
  const all = [...head, ...tail]
  if (all.some((g) => !/^[0-9a-fA-F]{1,4}$/.test(g))) return null
  if (halves.length === 1 ? head.length !== 8 : all.length > 7) return null
  const fill = Array<string>(8 - all.length).fill('0')
  let v = 0n
  for (const g of halves.length === 2 ? [...head, ...fill, ...tail] : head) {
    v = (v << 16n) | BigInt(`0x${g}`)
  }
  return v
}

function parseAddr(s: string): Addr | null {
  const t = s.trim()
  if (t.includes(':')) {
    const value = parseV6(t)
    return value === null ? null : { bits: 128, value }
  }
  const value = parseV4(t)
  return value === null ? null : { bits: 32, value }
}

export function inCidr(ip: string, cidr: string): boolean {
  const slash = cidr.split('/')
  if (slash.length !== 2 || !/^\d{1,3}$/.test(slash[1]!)) return false
  const net = parseAddr(slash[0]!)
  const addr = parseAddr(ip)
  if (!net || !addr || net.bits !== addr.bits) return false
  const len = Number(slash[1])
  if (len > net.bits) return false
  const shift = BigInt(net.bits - len)
  return net.value >> shift === addr.value >> shift
}

// Inclusive address count; 0n when malformed, mixed-family or reversed.
export function rangeSize(start: string, end: string): bigint {
  const a = parseAddr(start)
  const b = parseAddr(end)
  if (!a || !b || a.bits !== b.bits || b.value < a.value) return 0n
  return b.value - a.value + 1n
}

export function addrFamily(ip: string): 'ipv4' | 'ipv6' | null {
  const a = parseAddr(ip)
  return a ? (a.bits === 32 ? 'ipv4' : 'ipv6') : null
}
