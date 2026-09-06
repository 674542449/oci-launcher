<template>
  <img v-if="url" :src="url" :alt="label" :title="label" class="inline-block shrink-0 rounded-[2px] object-cover ring-1 ring-black/10" draggable="false" />
  <span v-else class="inline-block leading-none" :title="label" aria-hidden="true">{{ fallback }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { regionCountryCode, regionCountry, countryName } from '@/lib/regions'
import { flagUrl, flagEmoji } from '@/lib/flags'

const props = defineProps<{
  /** OCI region id, e.g. ap-tokyo-1 */
  region?: string
  /** ISO 3166-1 alpha-2 code; takes precedence over region */
  cc?: string
}>()

const code = computed(() => (props.cc || regionCountryCode(props.region) || '').toUpperCase())
const url = computed(() => flagUrl(code.value))
const label = computed(() => (props.cc ? countryName(props.cc) : regionCountry(props.region)) || code.value)
const fallback = computed(() => flagEmoji(code.value))
</script>
