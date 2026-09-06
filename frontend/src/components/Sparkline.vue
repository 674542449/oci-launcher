<template>
  <div class="relative select-none" @mouseleave="hover = -1">
    <svg
      :viewBox="`0 0 ${W} ${H}`"
      preserveAspectRatio="none"
      class="block h-[84px] w-full"
      role="img"
      :aria-label="ariaLabel"
      @mousemove="onMove"
      @touchstart.passive="onMove"
      @touchmove.passive="onMove"
    >
      <!-- Everything under the reclaim line: the zone Oracle counts as idle -->
      <template v-if="hasThreshold">
        <rect :x="PAD" :y="yOf(threshold!)" :width="W - PAD * 2" :height="H - PAD - yOf(threshold!)" style="fill: var(--c-danger)" fill-opacity="0.07" />
        <line :x1="PAD" :x2="W - PAD" :y1="yOf(threshold!)" :y2="yOf(threshold!)" style="stroke: var(--c-danger)" stroke-width="1" stroke-dasharray="4 3" vector-effect="non-scaling-stroke" />
      </template>

      <path v-if="areaPath" :d="areaPath" style="fill: var(--c-ink)" fill-opacity="0.06" />
      <path v-if="linePath" :d="linePath" fill="none" style="stroke: var(--c-ink-2)" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke" />
      <line :x1="PAD" :x2="W - PAD" :y1="H - PAD" :y2="H - PAD" style="stroke: var(--c-line)" stroke-width="1" vector-effect="non-scaling-stroke" />

      <g v-if="hover >= 0 && points[hover]">
        <line :x1="xOf(hover)" :x2="xOf(hover)" :y1="PAD" :y2="H - PAD" style="stroke: var(--c-ink-3)" stroke-width="1" stroke-dasharray="2 2" vector-effect="non-scaling-stroke" />
        <circle :cx="xOf(hover)" :cy="yOf(points[hover].v)" r="3" style="fill: var(--c-brand)" vector-effect="non-scaling-stroke" />
      </g>
    </svg>

    <div v-if="hover >= 0 && points[hover]" class="mono pointer-events-none absolute top-0 whitespace-nowrap rounded px-1.5 py-0.5 text-[11px] text-white" :style="tipStyle">
      {{ tipText }}
    </div>
    <div v-if="!points.length" class="caption absolute inset-0 flex items-center justify-center">无数据</div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

interface Point {
  t: string
  v: number
}

const props = withDefaults(
  defineProps<{
    points: Point[]
    /** fixed scale top (e.g. 100 for percentages); auto when omitted */
    max?: number
    /** dashed reference line, e.g. Oracle's 20 % reclaim threshold */
    threshold?: number
    unit?: string
    decimals?: number
    ariaLabel?: string
  }>(),
  { unit: '', decimals: 1, ariaLabel: '近 7 天趋势' },
)

const W = 300
const H = 84
const PAD = 4

const hover = ref(-1)

const hasThreshold = computed(() => props.threshold !== undefined && props.threshold > 0)

const scaleMax = computed(() => {
  if (props.max && props.max > 0) return props.max
  let m = 0
  for (const p of props.points) if (p.v > m) m = p.v
  if (hasThreshold.value) m = Math.max(m, props.threshold! * 1.25)
  return m > 0 ? m * 1.1 : 1
})

const xOf = (i: number) => {
  const n = props.points.length
  if (n <= 1) return PAD
  return PAD + ((W - PAD * 2) * i) / (n - 1)
}
const yOf = (v: number) => {
  const clamped = Math.min(Math.max(v, 0), scaleMax.value)
  return H - PAD - ((H - PAD * 2) * clamped) / scaleMax.value
}

const linePath = computed(() => {
  if (!props.points.length) return ''
  return props.points.map((p, i) => `${i === 0 ? 'M' : 'L'}${xOf(i).toFixed(1)} ${yOf(p.v).toFixed(1)}`).join(' ')
})
const areaPath = computed(() => {
  if (props.points.length < 2) return ''
  const last = props.points.length - 1
  return `${linePath.value} L${xOf(last).toFixed(1)} ${H - PAD} L${PAD} ${H - PAD} Z`
})

const onMove = (e: MouseEvent | TouchEvent) => {
  const n = props.points.length
  if (!n) return
  const svg = e.currentTarget as SVGSVGElement
  const rect = svg.getBoundingClientRect()
  const clientX = 'touches' in e ? e.touches[0]?.clientX : (e as MouseEvent).clientX
  if (clientX === undefined) return
  const ratio = Math.min(Math.max((clientX - rect.left) / rect.width, 0), 1)
  hover.value = Math.round(ratio * (n - 1))
}

const fmtTime = (t: string) => {
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return t
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:00`
}
const tipText = computed(() => {
  const p = props.points[hover.value]
  if (!p) return ''
  return `${fmtTime(p.t)} · ${p.v.toFixed(props.decimals)}${props.unit}`
})
const tipStyle = computed(() => {
  const n = props.points.length
  const ratio = n > 1 ? hover.value / (n - 1) : 0
  // keep the label inside the box: anchor left / center / right depending on position
  const translate = ratio < 0.2 ? '0' : ratio > 0.8 ? '-100%' : '-50%'
  return { left: `${(ratio * 100).toFixed(1)}%`, transform: `translateX(${translate})`, background: 'var(--c-ink)' }
})
</script>
