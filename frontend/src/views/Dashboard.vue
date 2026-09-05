<template>
  <div class="space-y-6">
    <!-- Header with Refresh Button -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 bg-white p-6 rounded-2xl border border-gray-200 shadow-sm">
      <div class="space-y-1">
        <div class="flex items-center space-x-3">
          <h2 class="text-xl font-bold text-gray-900">{{ currentProfile?.name || '未选择账号' }}</h2>
          <!-- Account Type Badge -->
          <span
            v-if="quota?.account_type"
            class="px-2.5 py-0.5 rounded-full text-xs font-semibold"
            :class="quota.account_type.effective_type === 'payg' ? 'bg-green-50 text-green-700 border border-green-200' : 'bg-blue-50 text-blue-700 border border-blue-200'"
          >
            {{ quota.account_type.effective_type === 'payg' ? '🟢 已升级按量付费号 (PAYG)' : '🔵 未升级免费号 (Always Free)' }}
          </span>
          <span v-if="currentProfile?.status === 'Active'" class="text-xs px-2 py-0.5 bg-emerald-50 text-emerald-600 rounded border border-emerald-200 font-medium">
            活跃正常
          </span>
          <span v-else-if="currentProfile?.status === 'Banned'" class="text-xs px-2 py-0.5 bg-red-50 text-red-600 rounded border border-red-200 font-bold animate-pulse">
            疑似封号/已停用
          </span>
        </div>
        <p class="text-xs text-gray-500">
          主区域 (Home Region): <span class="font-mono font-medium text-gray-700">{{ quota?.home_region || currentProfile?.region || '探测中' }}</span> ·
          租户 OCID: <span class="font-mono text-gray-400">{{ maskOCID(currentProfile?.tenancy_ocid) }}</span>
        </p>
      </div>

      <!-- Action Buttons -->
      <div class="flex items-center space-x-3">
        <n-button :loading="loading" type="primary" secondary @click="fetchData">
          🔄 手动刷新配额
        </n-button>
        <n-button type="primary" @click="$router.push('/launcher')">
          🚀 去抢机
        </n-button>
      </div>
    </div>

    <!-- Transparent Account Type Proof & Manual Override Card -->
    <div v-if="quota" class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-gray-100 pb-3">
        <div>
          <h3 class="text-sm font-bold text-gray-900">账号类型官方依据与判定透明公示</h3>
          <p class="text-xs text-gray-500">拒绝黑箱，所有免费额度判定均基于 Oracle 官方底层 API 与配额数据验证</p>
        </div>
        <!-- Manual Override Switcher -->
        <div class="flex items-center space-x-2">
          <span class="text-xs text-gray-600 font-medium">人工覆盖开关:</span>
          <n-select
            v-model:value="overrideValue"
            :options="overrideOptions"
            size="small"
            class="w-36"
            @update:value="saveOverride"
          />
        </div>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-4 text-xs">
        <div class="bg-gray-50 p-3.5 rounded-xl border border-gray-100 space-y-1">
          <span class="text-gray-500 font-medium">官方 API 判定结果</span>
          <div class="text-sm font-bold text-gray-800">{{ quota.account_type.detected_type }}</div>
          <p class="text-gray-400 text-[11px]">OneSubscription 官方订阅模型</p>
        </div>
        <div class="bg-gray-50 p-3.5 rounded-xl border border-gray-100 space-y-1">
          <span class="text-gray-500 font-medium">底层 A1 核心限额探测</span>
          <div class="text-sm font-bold text-gray-800">{{ quota.account_type.a1_core_limit || '未知' }} OCPU</div>
          <p class="text-gray-400 text-[11px]">standard-a1-core-count</p>
        </div>
        <div class="bg-gray-50 p-3.5 rounded-xl border border-gray-100 space-y-1 md:col-span-1">
          <span class="text-gray-500 font-medium">当前生效免费额度</span>
          <div class="text-sm font-bold text-red-600">{{ quota.total_free_ocpu }} OCPU / {{ quota.total_free_memory_gb }} GB</div>
          <p class="text-gray-400 text-[11px]">{{ quota.account_type.effective_type === 'payg' ? 'PAYG 升级号 (享 4C/24G 免费额度)' : 'Always Free 免费号 (享 4C/24G 免费额度)' }}</p>
        </div>
      </div>

      <!-- Detection Reason Banner -->
      <div class="bg-blue-50 border border-blue-100 p-3 rounded-xl flex items-center text-xs text-blue-900 space-x-2">
        <span>📋</span>
        <span><b>判定依据:</b> {{ quota.account_type.detection_reason }}</span>
      </div>
    </div>

    <!-- Resource Quotas Progress Dashboard -->
    <div v-if="quota" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
      <!-- 1. ARM A1 OCPU -->
      <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
        <div class="flex justify-between items-center">
          <span class="text-sm font-bold text-gray-700">ARM A1 算力核心</span>
          <span class="text-xs px-2 py-0.5 rounded bg-gray-100 text-gray-600 font-mono">{{ quota.used_a1_ocpu }} / {{ quota.total_free_ocpu }} OCPU</span>
        </div>
        <n-progress
          type="line"
          :percentage="Math.min(100, Math.round((quota.used_a1_ocpu / quota.total_free_ocpu) * 100))"
          :color="quota.used_a1_ocpu > quota.total_free_ocpu ? '#DC2626' : '#C74634'"
          :height="10"
          border-radius="5px"
        />
        <div class="flex justify-between text-xs text-gray-500">
          <span>已用: <b>{{ quota.used_a1_ocpu }}</b> 核</span>
          <span>剩余可开: <b class="text-emerald-600">{{ quota.available_a1_ocpu }}</b> 核</span>
        </div>
      </div>

      <!-- 2. ARM A1 Memory -->
      <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
        <div class="flex justify-between items-center">
          <span class="text-sm font-bold text-gray-700">ARM A1 内存容量</span>
          <span class="text-xs px-2 py-0.5 rounded bg-gray-100 text-gray-600 font-mono">{{ quota.used_a1_memory_gb }} / {{ quota.total_free_memory_gb }} GB</span>
        </div>
        <n-progress
          type="line"
          :percentage="Math.min(100, Math.round((quota.used_a1_memory_gb / quota.total_free_memory_gb) * 100))"
          :color="quota.used_a1_memory_gb > quota.total_free_memory_gb ? '#DC2626' : '#C74634'"
          :height="10"
          border-radius="5px"
        />
        <div class="flex justify-between text-xs text-gray-500">
          <span>已用: <b>{{ quota.used_a1_memory_gb }}</b> GB</span>
          <span>剩余可配: <b class="text-emerald-600">{{ quota.available_a1_memory_gb }}</b> GB</span>
        </div>
      </div>

      <!-- 3. Boot & Block Storage (200GB Limit) -->
      <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
        <div class="flex justify-between items-center">
          <span class="text-sm font-bold text-gray-700">总块存储与引导卷</span>
          <span class="text-xs px-2 py-0.5 rounded bg-gray-100 text-gray-600 font-mono">{{ quota.used_storage_gb }} / 200 GB</span>
        </div>
        <n-progress
          type="line"
          :percentage="Math.min(100, Math.round((quota.used_storage_gb / 200) * 100))"
          color="#0284C7"
          :height="10"
          border-radius="5px"
        />
        <div class="flex justify-between text-xs text-gray-500">
          <span>已占: <b>{{ quota.used_storage_gb }}</b> GB</span>
          <span>剩余免费: <b class="text-emerald-600">{{ quota.available_storage_gb }}</b> GB</span>
        </div>
      </div>

      <!-- 4. Monthly Outbound Traffic (10TB Free) -->
      <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
        <div class="flex justify-between items-center">
          <span class="text-sm font-bold text-gray-700">当月出站流量</span>
          <span
            class="text-xs px-2 py-0.5 rounded font-mono font-bold"
            :class="trafficInfo?.alert_level === 'critical' ? 'bg-red-100 text-red-700' : (trafficInfo?.alert_level === 'warning' ? 'bg-amber-100 text-amber-700' : 'bg-gray-100 text-gray-600')"
          >
            {{ trafficInfo?.used_tb?.toFixed(2) || '0.00' }} / 10 TB
          </span>
        </div>
        <n-progress
          type="line"
          :percentage="Math.min(100, Math.round(trafficInfo?.used_percent || 0))"
          :color="trafficInfo?.alert_level === 'critical' ? '#DC2626' : (trafficInfo?.alert_level === 'warning' ? '#F59E0B' : '#10B981')"
          :height="10"
          border-radius="5px"
        />
        <div class="flex justify-between text-xs text-gray-500">
          <span>消耗率: <b>{{ trafficInfo?.used_percent?.toFixed(1) || 0 }}%</b></span>
          <span>免费上限: <b>10 TB/月</b></span>
        </div>
      </div>
    </div>

    <!-- Estimated Fee Alert if Over Soft Line -->
    <div v-if="quota?.estimated_monthly_fee > 0" class="p-4 bg-amber-50 border border-amber-200 rounded-2xl flex items-center space-x-3 text-sm text-amber-800">
      <span class="text-2xl">⚠️</span>
      <div>
        <p class="font-bold">注意: 您的当前实例配置已超过免费软上限，产生按量计费预估！</p>
        <p class="text-xs text-amber-700">
          预估超出部分月费约为: <b>${{ quota.estimated_monthly_fee.toFixed(2) }}/月</b> ($0.01/核时 + $0.0015/GB时)。建议在「实例管理」中降配到免费水位。
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { useMessage } from 'naive-ui'

const profileStore = useProfileStore()
const message = useMessage()

const loading = ref(false)
const quota = ref<any>(null)
const trafficInfo = ref<any>(null)
const overrideValue = ref('auto')

const overrideOptions = [
  { label: '智能自动判定 (Auto)', value: 'auto' },
  { label: '强制 Always Free', value: 'free' },
  { label: '强制 PAYG (已升级)', value: 'payg' },
]

const currentProfile = computed(() => {
  return profileStore.profiles.find(p => p.id === profileStore.activeProfileId)
})

const maskOCID = (ocid?: string) => {
  if (!ocid || ocid.length < 20) return ocid || '未知'
  return ocid.substring(0, 12) + '...' + ocid.substring(ocid.length - 8)
}

const fetchData = async () => {
  if (!profileStore.activeProfileId) return
  loading.value = true
  try {
    const res: any = await api.get(`/quota?profile_id=${profileStore.activeProfileId}`)
    quota.value = res.summary
    overrideValue.value = currentProfile.value?.account_type_override || 'auto'

    // Fetch traffic
    const trafRes: any = await api.get(`/quota/traffic?profile_id=${profileStore.activeProfileId}`)
    trafficInfo.value = trafRes.traffic
  } catch (e: any) {
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
    message.success('人工覆盖配置已持久化保存！')
    await fetchData()
  } catch (e: any) {
    message.error(e.message)
  }
}

watch(() => profileStore.activeProfileId, () => {
  fetchData()
})

onMounted(() => {
  fetchData()
})
</script>
