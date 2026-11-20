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

export function actionLabel(command: string): string {
  const known = labels[command]
  if (known) return known
  const words = command.replace(/_/g, ' ').trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}
