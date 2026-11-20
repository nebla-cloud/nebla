import { formatDate } from '@/lib/format'
import type { CatalogAction, CatalogLocationState } from './queries'

export type CellTone = 'success' | 'neutral' | 'error' | 'muted'
export type CellPrimary = 'enable' | 'retry' | 'refresh'

export interface CellView {
  label: string
  tone: CellTone
  pulse: boolean
  primary: CellPrimary | null
  detail: string
}

function plural(n: number, word: string) {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

function lastBuilt(loc: CatalogLocationState): string | null {
  const dates = loc.nodes.map((n) => n.built_at).filter((d): d is string => !!d)
  return dates.length ? dates.sort().at(-1)! : null
}

// Retired entries only allow Disable, so cells without build rows show as not built.
export function cellView(loc: CatalogLocationState, retired = false): CellView {
  const v = baseView(loc, retired)
  if (!loc.available && loc.state !== 'unavailable' && loc.reason) {
    v.detail = `${v.detail} Can't build here right now: ${loc.reason}.`.trim()
  }
  return v
}

function baseView(loc: CatalogLocationState, retired: boolean): CellView {
  if (retired && loc.state === 'off') {
    return {
      label: 'Not built',
      tone: 'muted',
      pulse: false,
      primary: null,
      detail: "No longer in the catalog, so it can't be built here.",
    }
  }
  const nodes = loc.nodes.length
  const ready = loc.nodes.filter((n) => n.status === 'ready').length
  const failed = nodes - ready - loc.nodes.filter((n) => n.status === 'building').length
  switch (loc.state) {
    case 'off':
      return {
        label: 'Off',
        tone: 'neutral',
        pulse: false,
        primary: 'enable',
        detail: 'Not built here yet.',
      }
    case 'building':
      return {
        label: 'Building',
        tone: 'neutral',
        pulse: true,
        primary: null,
        detail: 'Nebla is downloading the image and making the template. This takes a few minutes.',
      }
    case 'ready': {
      const built = lastBuilt(loc)
      return {
        label: 'Ready',
        tone: 'success',
        pulse: false,
        primary: 'refresh',
        detail: built ? `Built ${formatDate(built)}.` : 'Ready to use.',
      }
    }
    case 'failed':
      return {
        label: 'Failed',
        tone: 'error',
        pulse: false,
        primary: 'retry',
        detail: 'The build failed. Retry builds it again.',
      }
    case 'partial':
      if (failed === 0) {
        return {
          label: 'Not on every node',
          tone: 'neutral',
          pulse: false,
          primary: 'retry',
          detail: `Ready on ${plural(ready, 'node')}, but not built on every node yet. Retry builds the missing ones.`,
        }
      }
      return {
        label: `${ready} of ${plural(nodes, 'node')}`,
        tone: 'neutral',
        pulse: false,
        primary: 'retry',
        detail: `Ready on ${ready} of ${plural(nodes, 'node')}. Retry rebuilds the ${failed === 1 ? 'one that failed' : 'ones that failed'}.`,
      }
    case 'retired':
      return {
        label: 'Retired',
        tone: 'muted',
        pulse: false,
        primary: null,
        detail: 'No longer in the catalog. You can only disable it.',
      }
    case 'unavailable':
      return {
        label: loc.reason ? `Unavailable: ${loc.reason}` : 'Unavailable',
        tone: 'muted',
        pulse: false,
        primary: null,
        detail: loc.reason ? `Can't build here: ${loc.reason}.` : "Can't build here right now.",
      }
    default:
      return { label: loc.state, tone: 'muted', pulse: false, primary: null, detail: '' }
  }
}

export function primaryAction(p: CellPrimary): CatalogAction {
  return p === 'retry' ? 'enable' : p
}

// Buttons for the popover, in display order.
export function cellActions(loc: CatalogLocationState, retired = false): CellPrimary[] {
  if (retired) return []
  switch (loc.state) {
    case 'off':
      return ['enable']
    case 'ready':
      return ['refresh']
    case 'failed':
      return ['retry']
    case 'partial':
      return loc.nodes.some((n) => n.status === 'ready') ? ['retry', 'refresh'] : ['retry']
    default:
      return []
  }
}

export function canDisable(loc: CatalogLocationState): boolean {
  if (loc.state === 'building' || loc.state === 'off') return false
  return loc.state === 'retired' || loc.nodes.length > 0
}
