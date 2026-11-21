import type { FirewallRule } from './queries'

export type Direction = FirewallRule['direction']
export type Protocol = FirewallRule['protocol']
export type RuleRow = { protocol: Protocol; port: string; ips: string; description: string }

export const PROTOCOLS: Protocol[] = ['tcp', 'udp', 'icmp', 'esp', 'gre']
export const ANY = '0.0.0.0/0, ::/0'

export const hasPort = (p: Protocol) => p === 'tcp' || p === 'udp'
const ipsKey = (d: Direction) => (d === 'in' ? 'source_ips' : 'destination_ips')

export function toRows(rules: FirewallRule[], d: Direction): RuleRow[] {
  return rules
    .filter((r) => r.direction === d)
    .map((r) => ({
      protocol: r.protocol,
      port: r.port ?? '',
      ips: (r[ipsKey(d)] ?? []).join(', '),
      description: r.description ?? '',
    }))
}

export const blankRow = (): RuleRow => ({ protocol: 'tcp', port: '', ips: ANY, description: '' })

const splitIps = (s: string) => s.split(/[\s,]+/).filter(Boolean)

function portError(p: string): string | null {
  const m = /^(\d{1,5})(?:-(\d{1,5}))?$/.exec(p.trim())
  const ok = (n: number) => n >= 1 && n <= 65535
  if (!m) return 'Use a port from 1 to 65535, or a range like 8000-8100.'
  const a = Number(m[1])
  const b = m[2] === undefined ? a : Number(m[2])
  return ok(a) && ok(b) && a <= b ? null : 'Use a port from 1 to 65535, or a range like 8000-8100.'
}

export function rowError(r: RuleRow): { port?: string; ips?: string; description?: string } {
  const e: { port?: string; ips?: string; description?: string } = {}
  if (hasPort(r.protocol)) {
    const pe = portError(r.port)
    if (pe) e.port = pe
  }
  const ips = splitIps(r.ips)
  if (!ips.length) e.ips = 'Add at least one address, like 0.0.0.0/0.'
  else if (ips.length > 50) e.ips = 'Use at most 50 addresses.'
  if (r.description.length > 255) e.description = 'Keep the description to 255 characters.'
  return e
}

export function toRule(r: RuleRow, d: Direction): FirewallRule {
  const description = r.description.trim()
  return {
    direction: d,
    protocol: r.protocol,
    ...(hasPort(r.protocol) ? { port: r.port.trim() } : {}),
    [ipsKey(d)]: splitIps(r.ips),
    ...(description ? { description } : {}),
  }
}
