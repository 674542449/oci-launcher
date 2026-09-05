<template>
  <header class="mb-6 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
    <div class="min-w-0">
      <p v-if="eyebrow" class="mb-1 text-xs font-medium uppercase tracking-wider text-ink-3">{{ eyebrow }}</p>
      <h1 class="text-xl font-semibold tracking-tight text-ink leading-7 sm:text-[22px]">{{ title }}</h1>
      <p v-if="description" class="mt-1 text-[13px] text-ink-3 leading-5">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2 sm:justify-end">
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
defineProps<{
  title: string
  description?: string
  eyebrow?: string
}>()
</script>
