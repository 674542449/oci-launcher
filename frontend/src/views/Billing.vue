<template>
  <div>
    <PageHeader title="账单" :description="currentProfile ? `${currentProfile.name} · 主区域 ${regionLabel(billing?.home_region || currentProfile.region)} · 数据来自 Oracle Usage API（Cost Analysis 同源）` : '尚未选择账号'">
      <template #actions>
        <n-button secondary :loading="loading" :disabled="!currentProfile" @click="fetchBilling(true)">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新
        </n-button>
      </template>
    </PageHeader>

    <!-- loading -->
    <div v-if="loading && !billing" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <n-skeleton v-for="i in 4" :key="i" height="112px" :sharp="false" />
    </div>

    <!-- error -->
    <div v-else-if="error" class="card card-pad">
      <EmptyState title="暂时无法读取账单数据" :description="error">
        <n-button type="primary" @click="fetchBilling(true)">重试</n-button>
      </EmptyState>
    </div>

    <template v-else-if="billing">
      <!-- KPI -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <div class="card card-pad">
          <div class="text-[13px] font-semibold text-ink">本月费用（至今）</div>
          <div class="caption">{{ billing.month_start }} 起，已结算 {{ billing.elapsed_days }} 天</div>
          <div class="mono mt-3 text-2xl font-semibold leading-8 text-ink">{{ money(billing.month_to_date) }}<span class="ml-1 text-sm font-normal text-ink-3">{{ billing.currency }}</span></div>
        </div>
        <div class="card card-pad">
          <div class="text-[13px] font-semibold text-ink">上月费用</div>
          <div class="caption">上一自然月合计</div>
          <div class="mono mt-3 text-2xl font-semibold leading-8 text-ink">{{ money(billing.last_month) }}<span class="ml-1 text-sm font-normal text-ink-3">{{ billing.currency }}</span></div>
        </div>
        <div class="card card-pad">
          <div class="text-[13px] font-semibold text-ink">本月预估</div>
          <div class="caption">按已结算天数的日均推算，共 {{ billing.days_in_month }} 天</div>
          <div class="mono mt-3 text-2xl font-semibold leading-8" :class="billing.projected_month > 0 ? 'text-warn' : 'text-ink'">{{ money(billing.projected_month) }}<span class="ml-1 text-sm font-normal text-ink-3">{{ billing.currency }}</span></div>
        </div>
        <div class="card card-pad">
          <div class="text-[13px] font-semibold text-ink">{{ billing.promotions.length ? '试用额度' : '账号类型' }}</div>
          <template v-if="billing.promotions.length">
            <div class="caption">{{ promotionStatus(promotion.status) }}<span v-if="promotion.time_started || promotion.time_expired"> · {{ promotion.time_started || '?' }} 至 {{ promotion.time_expired || '?' }}</span><span v-if="promotion.duration"> · {{ promotion.duration }} {{ durationUnit(promotion.duration_unit) }}</span></div>
            <div class="mono mt-3 text-2xl font-semibold leading-8 text-ink">{{ money(promotion.amount) }}<span class="ml-1 text-sm font-normal text-ink-3">{{ promotion.currency || billing.currency }}</span></div>
          </template>
          <template v-else>
            <div class="caption">{{ accountTypeText }}</div>
            <div class="mt-3"><span class="pill" :class="isPayg ? 'pill-warn' : 'pill-ok'">{{ isPayg ? '按量付费 PAYG' : 'Always Free' }}</span></div>
          </template>
        </div>
      </div>

      <!-- Daily trend -->
      <section class="card card-pad mt-4">
        <div class="card-head mb-3">
          <div>
            <h2 class="section-title">近 30 天每日费用</h2>
            <p class="caption">按 UTC 自然日汇总<span v-if="billing.data_as_of">，数据截至 {{ billing.data_as_of }}</span></p>
          </div>
          <span class="mono text-xs text-ink-3">合计 {{ money(dailyTotal) }} {{ billing.currency }}</span>
        </div>
        <Sparkline :points="dailyPoints" :unit="' ' + billing.currency" :decimals="2" aria-label="近 30 天每日费用" />
        <div class="mono mt-2 flex justify-between text-xs text-ink-3">
          <span>{{ billing.daily[0]?.date }}</span>
          <span>{{ billing.daily[billing.daily.length - 1]?.date }}</span>
        </div>
      </section>

      <!-- By service -->
      <section class="card mt-4 overflow-hidden">
        <div class="card-head card-pad pb-4">
          <div>
            <h2 class="section-title">按服务分类</h2>
            <p class="caption">本月与上月的费用构成，按本月费用降序</p>
          </div>
        </div>
        <EmptyState v-if="billing.services.length === 0" title="本月与上月均无计费项目" :description="billing.note || 'Always Free 资源不产生费用。'" />
        <div v-else class="tbl-wrap border-t border-line">
          <table class="tbl">
            <thead>
              <tr>
                <th>服务</th>
                <th class="text-right">本月费用</th>
                <th class="text-right">上月费用</th>
                <th class="text-right">本月占比</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in billing.services" :key="s.service">
                <td class="font-medium text-ink">{{ s.service }}</td>
                <td class="mono text-right text-ink">{{ money(s.month_to_date) }}</td>
                <td class="mono text-right text-ink-2">{{ money(s.last_month) }}</td>
                <td class="mono text-right text-ink-3">{{ share(s.month_to_date) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <p class="caption mt-4">
        费用为扣除折扣后的金额，不含税，以 Oracle 出具的账单为准；Oracle 按日结算用量，通常延迟约 24 小时。Always Free 资源不计费，费用通常来自超出免费额度的资源（高性能引导卷、超额存储、非主区域资源等）。
        <span v-if="billing.cached">当前为 30 分钟内的缓存结果，点击「刷新」重新读取。</span>
      </p>
    </template>

    <div v-else class="card card-pad">
      <EmptyState title="尚未选择账号" description="在左侧选择账号后查看账单。" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { NButton, NIcon, NSkeleton, useMessage } from 'naive-ui'
import { RefreshOutline } from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { regionLabel } from '@/lib/regions'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import Sparkline from '@/components/Sparkline.vue'

const profileStore = useProfileStore()
const message = useMessage()

const loading = ref(false)
const error = ref('')
const billing = ref<any>(null)

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))
const isPayg = computed(() => {
  const p: any = currentProfile.value
  const t = (p?.account_type_override && p.account_type_override !== 'auto' ? p.account_type_override : p?.detected_type) || ''
  return String(t).toLowerCase().includes('payg')
})
const accountTypeText = computed(() => (isPayg.value ? '升级号，超出免费额度的用量按量计费' : '免费号，无试用额度记录'))
const promotion = computed(() => {
  const list: any[] = billing.value?.promotions || []
  return list.find((p) => p.status === 'ACTIVE') || list[0] || {}
})

const money = (v: number) => (Number.isFinite(v) ? v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : '0.00')
const share = (v: number) => {
  const total = billing.value?.month_to_date || 0
  if (!total || !v) return '—'
  return `${((v / total) * 100).toFixed(1)}%`
}
const promotionStatus = (s: string) => ({ ACTIVE: '生效中', EXPIRED: '已过期', INITIALIZED: '未开始' } as Record<string, string>)[s] || s || '—'
const durationUnit = (u: string) => ({ DAYS: '天', MONTHS: '个月', HOURS: '小时' } as Record<string, string>)[(u || '').toUpperCase()] || (u || '')

const dailyPoints = computed(() => (billing.value?.daily || []).map((d: any) => ({ t: d.date + 'T00:00:00Z', v: d.amount })))
const dailyTotal = computed(() => (billing.value?.daily || []).reduce((acc: number, d: any) => acc + (d.amount || 0), 0))

const fetchBilling = async (force = false) => {
  if (!profileStore.activeProfileId) return
  loading.value = true
  error.value = ''
  try {
    const res: any = await api.get(`/billing?profile_id=${profileStore.activeProfileId}${force ? '&refresh=1' : ''}`)
    billing.value = res.billing
    if (force) message.success('账单数据已刷新')
  } catch (e: any) {
    error.value = e.message
    billing.value = null
  } finally {
    loading.value = false
  }
}

watch(
  () => profileStore.activeProfileId,
  () => {
    billing.value = null
    fetchBilling()
  },
)

onMounted(() => {
  fetchBilling()
})
</script>
