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

export interface ServerTypeTemplate {
  key: string
  name: string
  description: string
  cores: number
  memory_mb: number
  disk_gb: number
}

export const SERVER_TYPE_TEMPLATES: readonly ServerTypeTemplate[] = [
  {
    key: 'nano',
    name: 'nano',
    description: 'Tiny services, bastions and experiments',
    cores: 1,
    memory_mb: 512,
    disk_gb: 10,
  },
  {
    key: 'small',
    name: 'small',
    description: 'Small sites, bots and dev boxes',
    cores: 1,
    memory_mb: 1024,
    disk_gb: 25,
  },
  {
    key: 'medium',
    name: 'medium',
    description: 'Typical web apps and small databases',
    cores: 2,
    memory_mb: 4096,
    disk_gb: 50,
  },
  {
    key: 'large',
    name: 'large',
    description: 'Busier apps, CI runners and databases',
    cores: 4,
    memory_mb: 8192,
    disk_gb: 100,
  },
  {
    key: 'xl',
    name: 'xl',
    description: 'Heavy workloads and larger databases',
    cores: 8,
    memory_mb: 16384,
    disk_gb: 200,
  },
  {
    key: 'xxl',
    name: 'xxl',
    description: 'Large databases and build servers',
    cores: 16,
    memory_mb: 32768,
    disk_gb: 400,
  },
]
