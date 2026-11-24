// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import type { FloatingIp, PrimaryIp, Server, Volume } from '@/api/types'

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

const floatingIpStatuses: Record<
  FloatingIp['status'],
  { label: string; tone: StatusTone; pulse: boolean }
> = {
  active: { label: 'Active', tone: 'success', pulse: false },
  releasing: { label: 'Releasing', tone: 'busy', pulse: true },
  deleting: { label: 'Deleting', tone: 'busy', pulse: true },
}

export function floatingIpStatus(s: FloatingIp['status']): {
  label: string
  tone: StatusTone
  pulse: boolean
} {
  return { ...(floatingIpStatuses[s] ?? { label: s, tone: 'neutral', pulse: false }) }
}

export function isFloatingIpTransitional(s: FloatingIp['status']): boolean {
  return floatingIpStatus(s).pulse
}

// Primary IPs share the floating IP statuses they use.
export function primaryIpStatus(s: PrimaryIp['status']) {
  return floatingIpStatus(s)
}

export function isPrimaryIpTransitional(s: PrimaryIp['status']): boolean {
  return primaryIpStatus(s).pulse
}
