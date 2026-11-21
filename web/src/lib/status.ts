import type { Server, Volume } from '@/api/types'

export type StatusTone = 'success' | 'busy' | 'neutral' | 'error'

const statuses: Record<Server['status'], { label: string; tone: StatusTone; pulse: boolean }> = {
  running: { label: 'Running', tone: 'success', pulse: false },
  off: { label: 'Off', tone: 'neutral', pulse: false },
  initializing: { label: 'Initializing', tone: 'busy', pulse: true },
  starting: { label: 'Starting', tone: 'busy', pulse: true },
  stopping: { label: 'Stopping', tone: 'busy', pulse: true },
  rebuilding: { label: 'Rebuilding', tone: 'busy', pulse: true },
  deleting: { label: 'Deleting', tone: 'busy', pulse: true },
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

const volumeStatuses: Record<
  Volume['status'],
  { label: string; tone: StatusTone; pulse: boolean }
> = {
  creating: { label: 'Creating', tone: 'busy', pulse: true },
  available: { label: 'Not attached', tone: 'neutral', pulse: false },
  attached: { label: 'Attached', tone: 'success', pulse: false },
  deleting: { label: 'Deleting', tone: 'busy', pulse: true },
  missing: { label: 'Missing', tone: 'error', pulse: false },
  error: { label: 'Error', tone: 'error', pulse: false },
}

export function volumeStatus(s: Volume['status']): {
  label: string
  tone: StatusTone
  pulse: boolean
} {
  return { ...(volumeStatuses[s] ?? { label: s, tone: 'neutral', pulse: false }) }
}

export function isVolumeTransitional(s: Volume['status']): boolean {
  return volumeStatus(s).pulse
}
