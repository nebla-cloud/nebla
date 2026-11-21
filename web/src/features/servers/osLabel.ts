type OsImage = { name: string; os_flavor: string; os_version: string }

const FLAVORS: Record<string, string> = {
  debian: 'Debian',
  ubuntu: 'Ubuntu',
  rocky: 'Rocky Linux',
  alma: 'AlmaLinux',
  fedora: 'Fedora',
}

// The OS's display name and version, or null for a flavor we don't know.
export function osParts(image: OsImage): { name: string; version: string } | null {
  const name = FLAVORS[image.os_flavor.trim().toLowerCase()]
  return name ? { name, version: image.os_version.trim() } : null
}

export function osLabel(image: OsImage): string {
  const p = osParts(image)
  if (!p) return image.name
  return p.version ? `${p.name} ${p.version}` : p.name
}
