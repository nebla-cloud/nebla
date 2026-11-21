import type { ServerType } from '@/api/types'

export function formatMemory(mb: number): string {
  if (mb < 1024) return `${mb} MB`
  const gb = mb / 1024
  return `${Number.isInteger(gb) ? gb : Number(gb.toFixed(1))} GB`
}

const dateFormat = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
})

export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso))
}

export function typeSpec(t: ServerType): string {
  return `${t.cores} vCPU, ${formatMemory(t.memory_mb)}, ${t.disk_gb} GB`
}
