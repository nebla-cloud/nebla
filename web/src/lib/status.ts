import type { Server } from '@/api/types'

export type StatusTone = 'success' | 'neutral' | 'error'

const statuses: Record<Server['status'], { label: string; tone: StatusTone; pulse: boolean }> = {
  running: { label: 'Running', tone: 'success', pulse: false },
  off: { label: 'Off', tone: 'neutral', pulse: false },
  initializing: { label: 'Initializing', tone: 'neutral', pulse: true },
  starting: { label: 'Starting', tone: 'neutral', pulse: true },
  stopping: { label: 'Stopping', tone: 'neutral', pulse: true },
  rebuilding: { label: 'Rebuilding', tone: 'neutral', pulse: true },
  deleting: { label: 'Deleting', tone: 'neutral', pulse: true },
  missing: { label: 'Missing', tone: 'error', pulse: false },
  error: { label: 'Error', tone: 'error', pulse: false },
}

export function serverStatus(s: Server['status']): {
  label: string
  tone: StatusTone
  pulse: boolean
} {
  return { ...(statuses[s] ?? { label: s, tone: 'neutral', pulse: false }) }
}

export function isTransitional(s: Server['status']): boolean {
  return serverStatus(s).pulse
}
