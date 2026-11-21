<script setup lang="ts">
withDefaults(
  defineProps<{
    type: 'radio' | 'checkbox'
    name: string
    value: string
    disabled?: boolean
    variant?: 'default' | 'arch'
  }>(),
  { disabled: false, variant: 'default' },
)
const model = defineModel<unknown>()
</script>

<template>
  <label
    :data-variant="variant"
    class="nb-choice relative flex flex-col border bg-default px-3.5 text-default has-checked:border-(--nb-focus) has-checked:ring-3 has-checked:ring-(--nb-ring) has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-(--nb-focus)"
    :class="[
      variant === 'arch'
        ? 'gap-1.5 rounded-t-[28px] rounded-b-[12px] pt-4 pb-3'
        : 'gap-0.5 rounded-[12px] py-3',
      disabled ? 'cursor-not-allowed border-dashed border-default bg-muted' : 'cursor-pointer border-default',
    ]"
  >
    <input
      v-model="model"
      class="absolute inset-0 z-10 m-0 cursor-[inherit] appearance-none rounded-[inherit] opacity-0"
      :type="type"
      :name="name"
      :value="value"
      :disabled="disabled"
    />
    <span class="text-sm font-bold text-highlighted"><slot /></span>
    <span v-if="$slots.meta" class="text-[12.5px] text-muted"><slot name="meta" /></span>
  </label>
</template>

<style scoped>
.nb-choice[data-variant='arch']:has(:checked) {
  border-color: transparent;
  background:
    linear-gradient(var(--ui-bg), var(--ui-bg)) padding-box,
    var(--nb-gradient) border-box;
}
</style>
