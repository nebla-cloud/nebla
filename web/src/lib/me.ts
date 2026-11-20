import { ref } from 'vue'
import type { User } from '@/api/types'

export const me = ref<User | null>()
