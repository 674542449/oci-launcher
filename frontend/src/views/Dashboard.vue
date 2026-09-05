<template>
  <div>
    <PageHeader eyebrow="额度仪表盘" :title="currentProfile?.name || '尚未选择账号'">
      <template #actions>
        <n-button secondary :loading="loading" :disabled="!currentProfile" @click="fetchData">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新配额
        </n-button>
        <n-button type="primary" :disabled="!currentProfile" @click="$router.push('/launcher')">
          <template #icon><n-icon><RocketOutline /></n-icon></template>
          创建实例
        </n-button>
      </template>
    </PageHeader>

    <!-- No accounts yet -->
    <div v-if="!profileStore.loading && profileStore.profiles.length === 0" class="card">
      <EmptyState title="还没有导入任何 OCI 账号" description="先导入一个账号的 API 凭据，仪表盘会实时读取它的免费额度使用情况。">
        <n-button type="primary" @click="$router.push('/profiles')">导入账号</n-button>
      </EmptyState>
    </div>

    <template v-else>
      <!-- Identity strip -->
      <div class="card card-pad mb-6">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div class="flex flex-wrap items-center gap-2">
            <StatusPill :state="currentProfile?.status || 'UNKNOWN'" :label="profileStatusLabel" />
            <span v-if="quota" class="pill" :class="quota.account_type.effective_type === 'payg' ? 'pill-info' : 'pill-muted'">
              {{ quota.account_type.effective_type === 'payg' ? '按量付费 PAYG' : 'Always Free' }}
            </span>
            <span class="caption">
              主区域 <span class="mono text-ink-2">{{ quota?.home_region || currentProfile?.region || '—' }}</span>
              <span class="mx-1.5 text-line-strong">·</span>
              租户 <span class="mono text-ink-2" :title="currentProfile?.tenancy_ocid">{{ maskOCID(currentProfile?.tenancy_ocid) }}</span>
            </span>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-xs text-ink-3">账号类型覆盖</span>
            <n-select
              v-model:value="overrideValue"
              :options="overrideOptions"
              size="small"
              class="w-44"
              :disabled="!currentProfile"
              @update:value="saveOverride"
            />
          </div>
        </div>
      </div>

      <!-- Loading skeleton -->
      <div v-if="loading && !quota" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <div v-for="i in 4" :key="i" class="card card-pad space-y-4">
          <n-skeleton text :repeat="1" width="40%" />
          <n-skeleton height="10px" :sharp="false" />
          <n-skeleton text :repeat="1" width="70%" />
        </div>
      </div>

      <template v-else-if="quota">
        <!-- Free-tier allotment meters: each cell is one unit of the allotment -->
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <Meter
            label="ARM A1 算力"
            :hint="`VM.Standard.A1.Flex · ${quota.account_type.effective_type === 'payg' ? '升级号' : '免费号'}免费 ${quota.total_free_ocpu} OCPU`"
            :value="quota.used_a1_ocpu"
            :max="quota.total_free_ocpu"
            unit="OCPU"
            :segments="quota.total_free_ocpu || 4"
          />
          <Meter
            label="ARM A1 内存"
            hint="每 OCPU 建议搭配 6 GB"
            :value="quota.used_a1_memory_gb"
            :max="quota.total_free_memory_gb"
            unit="GB"
            :segments="quota.total_free_memory_gb || 24"
            :group-size="6"
          />
          <Meter
            label="块存储与引导卷"
            hint="引导卷 + 块存储合计 200 GB"
            :value="quota.used_storage_gb"
            :max="quota.total_storage_gb || 200"
            unit="GB"
            :segments="8"
            :group-size="2"
          />
          <Meter
            label="当月出站流量"
            :hint="trafficInfo?.alert_description || '每月免费 10 TB'"
            :value="trafficInfo?.used_tb || 0"
            :max="trafficInfo?.max_tb || 10"
            unit="TB"
            :segments="10"
            :decimals="2"
            :warn-at="0.8"
          >
            <template #left>
              <span>消耗 <b class="mono font-semibold text-ink-2">{{ (trafficInfo?.used_percent || 0).toFixed(1) }}%</b></span>
            </template>
          </Meter>
        </div>

        <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
          <!-- Micro slots -->
          <Meter
            label="AMD Micro 实例"
            hint="VM.Standard.E2.1.Micro · 免费 2 台"
            :value="quota.micro_count"
            :max="quota.max_micro_count || 2"
            unit="台"
            :segments="quota.max_micro_count || 2"
            :warn-at="1.01"
          />

          <!-- How the account type was decided -->
          <div class="card card-pad lg:col-span-2">
            <div class="card-head mb-4">
              <div>
                <div class="section-title">账号类型判定依据</div>
                <p class="caption">基于 Oracle 官方 API 与配额数据，可在上方手动覆盖。</p>
              </div>
            </div>
            <dl class="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-3">
              <div class="rounded-lg border border-line bg-surface-2 px-3.5 py-3">
                <dt class="caption">账号判定结果</dt>
                <dd class="mono mt-0.5 text-[15px] font-semibold text-ink">{{ detectedLabel }}</dd>
                <dd class="caption mt-0.5">{{ quota.account_type.detection_source === 'subscription' ? '来源：Organizations 订阅接口' : quota.account_type.detection_source === 'limits' ? '来源：服务限额推断（订阅接口不可用）' : '来源：未知' }}</dd>
              </div>
              <div class="rounded-lg border border-line bg-surface-2 px-3.5 py-3">
                <dt class="caption">A1 核心限额</dt>
                <dd class="mono mt-0.5 text-[15px] font-semibold text-ink">{{ quota.account_type.a1_core_limit || '—' }} <span class="text-xs font-normal text-ink-3">OCPU</span></dd>
                <dd class="caption mt-0.5 mono">standard-a1-core-count</dd>
              </div>
              <div class="rounded-lg border border-line bg-surface-2 px-3.5 py-3">
                <dt class="caption">当前生效免费额度</dt>
                <dd class="mono mt-0.5 text-[15px] font-semibold text-ink">{{ quota.total_free_ocpu }} OCPU <span class="text-ink-3">/</span> {{ quota.total_free_memory_gb }} GB</dd>
                <dd class="caption mt-0.5">{{ quota.account_type.effective_type === 'payg' ? 'PAYG 升级号' : 'Always Free 免费号' }}</dd>
              </div>
            </dl>
            <div v-if="quota.account_type.detection_reason" class="notice notice-info mt-4">
              <n-icon size="18" class="mt-0.5 shrink-0"><InformationCircleOutline /></n-icon>
              <span>{{ quota.account_type.detection_reason }}</span>
            </div>
          </div>
        </div>

        <!-- Over the free line -->
        <div v-if="quota.estimated_monthly_fee > 0" class="notice notice-warn mt-4">
          <n-icon size="18" class="mt-0.5 shrink-0"><WarningOutline /></n-icon>
          <div>
            <p class="font-semibold">当前配置已超过免费上限，会产生按量计费。</p>
            <p class="mt-0.5">
              预估超出部分约 <b class="mono">${{ quota.estimated_monthly_fee.toFixed(2) }}</b> / 月（$0.01 每核时 + $0.0015 每 GB 时）。可在「实例」页面降配回免费水位。
            </p>
          </div>
        </div>
      </template>

      <!-- Failed to load -->
      <div v-else-if="!loading && currentProfile" class="card">
        <EmptyState title="暂时读不到这个账号的配额" description="请检查账号凭据是否有效，或稍后重试。">
          <n-button secondary @click="fetchData">重试</n-button>
        </EmptyState>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { NButton, NIcon, NSelect, NSkeleton, useMessage } from 'naive-ui'
import { RefreshOutline, RocketOutline, InformationCircleOutline, WarningOutline } from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import Meter from '@/components/Meter.vue'
import StatusPill from '@/components/StatusPill.vue'
import EmptyState from '@/components/EmptyState.vue'

const profileStore = useProfileStore()
const message = useMessage()

const loading = ref(false)
const quota = ref<any>(null)
const trafficInfo = ref<any>(null)
const overrideValue = ref('auto')

const overrideOptions = [
  { label: '自动判定', value: 'auto' },
  { label: '强制 Always Free', value: 'free' },
  { label: '强制 PAYG', value: 'payg' },
]

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))

const detectedLabel = computed(() => {
  const t = quota.value?.account_type?.detected_type
  if (t === 'PAYG') return '已升级 (PAYG)'
  if (t === 'FREE_TIER') return '免费号 (Free Tier)'
  return '未能判定'
})

const profileStatusLabel = computed(() => {
  const s = currentProfile.value?.status
  if (s === 'Active') return '账号正常'
  if (s === 'Banned') return '疑似封号 / 已停用'
  if (s === 'Invalid') return '凭据无效'
  return s || '状态未知'
})

const maskOCID = (ocid?: string) => {
  if (!ocid || ocid.length < 20) return ocid || '—'
  return ocid.substring(0, 12) + '…' + ocid.substring(ocid.length - 8)
}

const fetchData = async () => {
  if (!profileStore.activeProfileId) return
  loading.value = true
  try {
    const res: any = await api.get(`/quota?profile_id=${profileStore.activeProfileId}`)
    quota.value = res.summary
    overrideValue.value = currentProfile.value?.account_type_override || 'auto'
    try {
      const trafRes: any = await api.get(`/quota/traffic?profile_id=${profileStore.activeProfileId}`)
      trafficInfo.value = trafRes.traffic
    } catch (e: any) {
      trafficInfo.value = null
      message.warning('流量数据暂时不可用：' + e.message)
    }
  } catch (e: any) {
    quota.value = null
    message.error(e.message)
  } finally {
    loading.value = false
  }
}

const saveOverride = async (val: string) => {
  if (!currentProfile.value) return
  try {
    await api.post('/profiles/update', {
      id: currentProfile.value.id,
      account_type_override: val,
      tags: currentProfile.value.tags,
      notes: currentProfile.value.notes,
    })
    message.success('账号类型覆盖已保存')
    await profileStore.fetchProfiles()
    await fetchData()
  } catch (e: any) {
    message.error(e.message)
  }
}

watch(
  () => profileStore.activeProfileId,
  () => {
    quota.value = null
    trafficInfo.value = null
    fetchData()
  },
)

onMounted(() => {
  fetchData()
})
</script>
