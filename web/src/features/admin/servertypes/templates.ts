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
    name: 'Nano',
    description: 'Tiny services, bastions and experiments',
    cores: 1,
    memory_mb: 512,
    disk_gb: 10,
  },
  {
    key: 'small',
    name: 'Small',
    description: 'Small sites, bots and dev boxes',
    cores: 1,
    memory_mb: 1024,
    disk_gb: 25,
  },
  {
    key: 'medium',
    name: 'Medium',
    description: 'Typical web apps and small databases',
    cores: 2,
    memory_mb: 4096,
    disk_gb: 50,
  },
  {
    key: 'large',
    name: 'Large',
    description: 'Busier apps, CI runners and databases',
    cores: 4,
    memory_mb: 8192,
    disk_gb: 100,
  },
  {
    key: 'xl',
    name: 'XL',
    description: 'Heavy workloads and larger databases',
    cores: 8,
    memory_mb: 16384,
    disk_gb: 200,
  },
  {
    key: 'xxl',
    name: 'XXL',
    description: 'Large databases and build servers',
    cores: 16,
    memory_mb: 32768,
    disk_gb: 400,
  },
]
