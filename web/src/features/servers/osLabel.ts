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

// A server's OS label; it has no image when built from a snapshot whose image is gone.
export function serverOsLabel(s: {
  image: OsImage | null
  os_flavor: string
  os_version: string
}): string {
  return osLabel(s.image ?? { name: '', os_flavor: s.os_flavor, os_version: s.os_version })
}

export function osLabel(image: OsImage): string {
  const p = osParts(image)
  if (!p) return image.name
  return p.version ? `${p.name} ${p.version}` : p.name
}
