<template>
  <div class="card card-pad flex flex-col gap-4">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="text-[13px] font-semibold text-ink leading-5">{{ label }}</div>
        <div v-if="hint" class="caption truncate">{{ hint }}</div>
      </div>
      <div class="mono shrink-0 text-right leading-5">
        <span class="text-lg font-semibold" :class="toneText">{{ fmt(value) }}</span>
        <span class="text-xs text-ink-3"> / {{ fmt(max) }} {{ unit }}</span>
      </div>
    </div>

    <!-- Segmented meter: each cell is one unit of the free allotment -->
    <div
      class="flex gap-[3px]"
      role="meter"
      :aria-label="label"
      :aria-valuemin="0"
      :aria-valuemax="max"
      :aria-valuenow="value"
      :aria-valuetext="`${fmt(value)} / ${fmt(max)} ${unit}`"
    >
      <template v-for="(g, gi) in groups" :key="gi">
        <div class="flex flex-1 gap-[3px]" :class="gi > 0 ? 'ml-[5px]' : ''">
          <span
            v-for="i in g"
            :key="i"
            class="h-2.5 flex-1 rounded-[3px] transition-[background] duration-300"
            :style="cellStyle(cellIndex(gi, i))"
          />
        </div>
      </template>
    </div>

    <div class="flex items-center justify-between text-xs text-ink-3 leading-5">
      <slot name="left">
        <span>已用 <b class="mono font-semibold text-ink-2">{{ fmt(value) }}</b> {{ unit }}</span>
      </slot>
      <slot name="right">
        <span v-if="!over">剩余 <b class="mono font-semibold" :class="remainingClass">{{ fmt(remaining) }}</b> {{ unit }}</span>
        <span v-else class="font-semibold text-danger">超出 {{ fmt(value - max) }} {{ unit }}</span>
      </slot>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    label: string
    hint?: string
    value: number
    max: number
    unit?: string
    /** total number of cells */
    segments?: number
    /** cells per visual group (defaults to all cells in one group) */
    groupSize?: number
    /** ratio at which the meter turns amber */
    warnAt?: number
    decimals?: number
  }>(),
  { unit: '', segments: 10, groupSize: 0, warnAt: 0.85, decimals: 0 },
)

const safeMax = computed(() => (props.max > 0 ? props.max : 1))
const ratio = computed(() => Math.max(0, props.value / safeMax.value))
const over = computed(() => props.value > props.max + 1e-9)
const remaining = computed(() => Math.max(0, props.max - props.value))

const tone = computed(() => (over.value ? 'danger' : ratio.value >= props.warnAt ? 'warn' : 'ink'))
const toneText = computed(() => ({ danger: 'text-danger', warn: 'text-warn', ink: 'text-ink' })[tone.value])
const remainingClass = computed(() => (remaining.value <= 0 ? 'text-ink-3' : 'text-ok'))

const groups = computed(() => {
  const size = props.groupSize > 0 ? props.groupSize : props.segments
  const out: number[] = []
  let left = props.segments
  while (left > 0) {
    out.push(Math.min(size, left))
    left -= Math.min(size, left)
  }
  return out
})
const cellIndex = (gi: number, i: number) => {
  let idx = 0
  for (let k = 0; k < gi; k++) idx += groups.value[k]
  return idx + (i - 1)
}

const fillColor = computed(() => ({ danger: 'var(--c-danger)', warn: 'var(--c-warn)', ink: 'var(--c-ink)' })[tone.value])

const cellStyle = (idx: number) => {
  const filled = Math.min(1, Math.max(0, ratio.value * props.segments - idx))
  if (filled <= 0) return { background: 'var(--c-line)' }
  if (filled >= 1) return { background: fillColor.value }
  const pct = Math.round(filled * 100)
  return { background: `linear-gradient(90deg, ${fillColor.value} ${pct}%, var(--c-line) ${pct}%)` }
}

const fmt = (n: number) => {
  if (n === undefined || n === null || Number.isNaN(n)) return '0'
  const d = props.decimals
  return Number.isInteger(n) && d === 0 ? String(n) : n.toFixed(d || (Number.isInteger(n) ? 0 : 1))
}
</script>
