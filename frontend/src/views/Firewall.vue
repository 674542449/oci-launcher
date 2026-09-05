<template>
  <div class="space-y-6">
    <!-- Header with 3 Core One-Click Buttons -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm flex flex-col lg:flex-row justify-between items-start lg:items-center gap-4">
      <div>
        <h2 class="text-xl font-bold text-gray-900">防火墙与安全列表 (Security List)</h2>
        <p class="text-xs text-gray-500">子网安全组入站出站规则可视化 · 解决连接不通与端口阻断</p>
      </div>

      <!-- Quick Action Buttons -->
      <div class="flex flex-wrap gap-2.5">
        <n-button type="success" secondary :loading="operating" @click="handleAllowAll">
          🟢 一键放通所有规则 (IPv4/v6 全通)
        </n-button>
        <n-button type="warning" secondary :loading="operating" @click="handleAllowCloudflare">
          🟠 一键放通 Cloudflare CDN IP 段 (80/443)
        </n-button>
        <n-button type="error" secondary :loading="operating" @click="handleClearAll">
          🔴 一键清空所有入站规则
        </n-button>
      </div>
    </div>

    <!-- Subnet & Security List Selector -->
    <div class="bg-white p-4 rounded-xl border border-gray-200 shadow-sm flex flex-col sm:flex-row items-center gap-4 text-xs">
      <div class="flex items-center space-x-2 w-full sm:w-auto">
        <span class="text-gray-500 font-medium whitespace-nowrap">选择目标网络 (VCN):</span>
        <n-select v-model:value="selectedVCN" :options="vcnOptions" size="small" class="w-56" @update:value="onVCNChange" />
      </div>
      <div class="flex items-center space-x-2 w-full sm:w-auto">
        <span class="text-gray-500 font-medium whitespace-nowrap">关联安全列表:</span>
        <span class="font-mono text-gray-700 bg-gray-50 px-2 py-1 rounded border">{{ selectedSecListID || '未关联' }}</span>
      </div>
      <div class="sm:ml-auto">
        <n-button size="small" :loading="loading" @click="fetchRules">刷新规则</n-button>
      </div>
    </div>

    <!-- Rules Table -->
    <div class="bg-white rounded-2xl border border-gray-200 shadow-sm overflow-hidden p-6 space-y-4">
      <h3 class="text-sm font-bold text-gray-900">入站安全规则清单 (Ingress Rules)</h3>

      <div v-if="loading" class="py-12 text-center text-gray-400">
        正在拉取安全列表规则...
      </div>
      <div v-else-if="rules.length === 0" class="py-12 text-center text-gray-400">
        当前安全列表中无任何入站规则（外部网络无法连接任何端口）
      </div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-200 text-left text-xs">
          <thead class="bg-gray-50 text-gray-500 font-medium">
            <tr>
              <th class="px-4 py-3">协议 (Protocol)</th>
              <th class="px-4 py-3">来源 IP / CIDR</th>
              <th class="px-4 py-3">目标端口范围</th>
              <th class="px-4 py-3">规则说明</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 text-gray-700">
            <tr v-for="(r, idx) in rules" :key="idx" class="hover:bg-gray-50/80">
              <td class="px-4 py-3">
                <span class="px-2 py-0.5 rounded text-xs font-bold font-mono" :class="r.protocol === 'TCP' ? 'bg-blue-50 text-blue-700' : 'bg-purple-50 text-purple-700'">
                  {{ r.protocol }}
                </span>
              </td>
              <td class="px-4 py-3 font-mono font-bold text-gray-900">{{ r.source }}</td>
              <td class="px-4 py-3 font-mono text-emerald-600 font-semibold">{{ r.port_range }}</td>
              <td class="px-4 py-3 text-gray-500">{{ r.description || '-' }}</td>
            </tr>
          </tbody>
        </table>
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
const operating = ref(false)
const selectedVCN = ref<string | null>(null)
const selectedSecListID = ref('')
const vcnOptions = ref<any[]>([])
const rules = ref<any[]>([])

const currentProfile = computed(() => {
  return profileStore.profiles.find(p => p.id === profileStore.activeProfileId)
})

const loadVCNs = async () => {
  if (!profileStore.activeProfileId) return
  try {
    const res: any = await api.get(`/network/vcns?profile_id=${profileStore.activeProfileId}`)
    vcnOptions.value = (res.vcns || []).map((v: any) => ({
      label: `${v.display_name} (${v.cidr_block})`,
      value: v.ocid,
    }))
    if (vcnOptions.value.length > 0) {
      selectedVCN.value = vcnOptions.value[0].value
      await onVCNChange()
    }
  } catch (e: any) {
    message.error(e.message)
  }
}

const onVCNChange = async () => {
  if (!selectedVCN.value || !profileStore.activeProfileId) return
  try {
    const res: any = await api.get(`/network/subnets?profile_id=${profileStore.activeProfileId}&vcn_id=${selectedVCN.value}`)
    if (res.subnets && res.subnets.length > 0) {
      selectedSecListID.value = res.subnets[0].security_list_id
      await fetchRules()
    }
  } catch (e: any) {
    message.error(e.message)
  }
}

const fetchRules = async () => {
  if (!profileStore.activeProfileId || !selectedSecListID.value) return
  loading.value = true
  try {
    const res: any = await api.get(`/network/security-rules?profile_id=${profileStore.activeProfileId}&security_list_id=${selectedSecListID.value}`)
    rules.value = res.rules || []
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loading.value = false
  }
}

const handleAllowAll = async () => {
  if (!confirm('确定要一键放行 IPv4 (0.0.0.0/0) 与 IPv6 (::/0) 全端口全协议吗？')) return
  operating.value = true
  try {
    const res: any = await api.post('/network/allow-all', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      security_list_id: selectedSecListID.value,
    })
    message.success(res.message)
    await fetchRules()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    operating.value = false
  }
}

const handleClearAll = async () => {
  if (!confirm('【高危操作】确定要清空所有入站安全规则吗？清空后外部将无法连接任何端口！')) return
  operating.value = true
  try {
    const res: any = await api.post('/network/clear-all', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      security_list_id: selectedSecListID.value,
    })
    message.success(res.message)
    await fetchRules()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    operating.value = false
  }
}

const handleAllowCloudflare = async () => {
  operating.value = true
  try {
    const res: any = await api.post('/network/allow-cloudflare', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      security_list_id: selectedSecListID.value,
    })
    message.success(res.message)
    await fetchRules()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    operating.value = false
  }
}

watch(() => profileStore.activeProfileId, () => {
  loadVCNs()
})

onMounted(() => {
  loadVCNs()
})
</script>
