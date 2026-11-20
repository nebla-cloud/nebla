const labels: Record<string, string> = {
  create_server: 'Creating',
  delete_server: 'Deleting',
  start_server: 'Starting',
  stop_server: 'Powering off',
  shutdown_server: 'Shutting down',
  reboot_server: 'Rebooting',
  reset_server: 'Resetting',
  rebuild_server: 'Rebuilding',
  change_server_type: 'Changing type',
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
