<script lang="ts">
import { defineComponent, type PropType } from 'vue'
import type { Server } from '@/api/types'

export default defineComponent({
  props: { server: { type: Object as PropType<Server>, required: true } },
  render: () => null,
})
</script>
