<template>
  <div class="flex flex-col items-center justify-center px-6 py-14 text-center">
    <div class="mb-3 flex h-11 w-11 items-center justify-center rounded-full bg-surface-2 text-ink-3">
      <slot name="icon">
        <n-icon size="22"><FileTrayOutline /></n-icon>
      </slot>
    </div>
    <p class="text-[14px] font-medium text-ink">{{ title }}</p>
    <p v-if="description" class="mt-1 max-w-sm text-[13px] text-ink-3 leading-5">{{ description }}</p>
    <div v-if="$slots.default" class="mt-4">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { NIcon } from 'naive-ui'
import { FileTrayOutline } from '@vicons/ionicons5'

defineProps<{
  title: string
  description?: string
}>()
</script>
