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

const labels: Record<string, string> = {
  create_server: 'Creating',
  delete_server: 'Deleting',
  start_server: 'Starting',
  stop_server: 'Powering off',
  shutdown_server: 'Shutting down',
  reboot_server: 'Rebooting',
  reset_server: 'Resetting',
  rebuild_server: 'Rebuilding',
  change_server_type: 'Resizing',
  apply_firewalls: 'Applying firewalls',
  create_volume: 'Creating volume',
  attach_volume: 'Attaching volume',
  detach_volume: 'Detaching volume',
  resize_volume: 'Resizing volume',
  delete_volume: 'Deleting volume',
  create_snapshot: 'Taking snapshot',
  delete_snapshot: 'Deleting snapshot',
  assign_floating_ip: 'Assigning floating IP',
  unassign_floating_ip: 'Unassigning floating IP',
  delete_floating_ip: 'Deleting floating IP',
  create_backup: 'Backing up',
  restore_backup: 'Restoring',
  delete_backup: 'Deleting backup',
  attach_to_network: 'Attaching to network',
  detach_from_network: 'Detaching from network',
  assign_primary_ip: 'Assigning primary IP',
  unassign_primary_ip: 'Unassigning primary IP',
}

const done: Record<string, string> = {
  create_server: 'created',
  start_server: 'started',
  stop_server: 'powered off',
  shutdown_server: 'shut down',
  reboot_server: 'rebooted',
  reset_server: 'reset',
  rebuild_server: 'rebuilt',
  change_server_type: 'resized',
  delete_server: 'deleted',
  restore_backup: 'restored',
  delete_backup: 'backup deleted',
}

const titles: Record<string, string> = {
  create_server: 'Create',
  delete_server: 'Delete',
  start_server: 'Power on',
  stop_server: 'Power off',
  shutdown_server: 'Shut down',
  reboot_server: 'Reboot',
  reset_server: 'Reset',
  rebuild_server: 'Rebuild',
  change_server_type: 'Resize',
  apply_firewalls: 'Apply firewalls',
  create_snapshot: 'Take snapshot',
  delete_snapshot: 'Delete snapshot',
  assign_floating_ip: 'Assign floating IP',
  unassign_floating_ip: 'Unassign floating IP',
  delete_floating_ip: 'Delete floating IP',
  create_backup: 'Back up',
  restore_backup: 'Restore',
  delete_backup: 'Delete backup',
  attach_to_network: 'Attach to network',
  detach_from_network: 'Detach from network',
  assign_primary_ip: 'Assign primary IP',
  unassign_primary_ip: 'Unassign primary IP',
}

function sentence(command: string): string {
  const words = command.replace(/_/g, ' ').trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

export function actionLabel(command: string): string {
  return labels[command] ?? sentence(command)
}

export function actionDone(command: string): string | null {
  return done[command] ?? null
}

export function actionTitle(command: string): string {
  return titles[command] ?? sentence(command)
}

export function actionStatus(a: { status: string; progress: number }): string {
  if (a.status === 'running') return `Running ${Math.round(a.progress)}%`
  return a.status === 'success' ? 'Done' : 'Failed'
}

export function relativeTime(iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  return `${Math.floor(s / 86400)} d ago`
}

export function actionDot(a: { status: string }): string {
  if (a.status === 'error') return 'bg-[var(--nb-err)]'
  return a.status === 'running'
    ? 'bg-[var(--nb-busy)] motion-safe:animate-pulse'
    : 'bg-[var(--nb-run)]'
}
