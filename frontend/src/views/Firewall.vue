<template>
  <div>
    <PageHeader title="防火墙" description="查看并批量修改子网安全列表（Security List）的入站规则。">
      <template #actions>
        <n-button secondary :loading="operating" :disabled="!selectedSecListID" @click="handleAllowCloudflare">
          <template #icon><n-icon><ShieldCheckmarkOutline /></n-icon></template>
          放通 Cloudflare 80/443
        </n-button>
        <n-button secondary type="success" :loading="operating" :disabled="!selectedSecListID" @click="handleAllowAll">放通全部</n-button>
        <n-button secondary type="error" :loading="operating" :disabled="!selectedSecListID" @click="handleClearAll">清空入站规则</n-button>
      </template>
    </PageHeader>

    <!-- Target selector -->
    <div class="card card-pad mb-4">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div>
          <span class="label">VCN</span>
          <n-select v-model:value="selectedVCN" :options="vcnOptions" placeholder="选择 VCN" :loading="loadingNets" @update:value="onVCNChange" />
        </div>
        <div>
          <span class="label">子网</span>
          <n-select v-model:value="selectedSubnet" :options="subnetOptions" placeholder="选择子网" :disabled="!selectedVCN" @update:value="onSubnetChange" />
        </div>
        <div class="min-w-0">
          <span class="label">安全列表</span>
          <div class="mono flex h-[34px] items-center truncate rounded-lg border border-line bg-surface-2 px-3 text-xs text-ink-2" :title="selectedSecListID">
            {{ selectedSecListID ? maskOCID(selectedSecListID) : '未关联' }}
          </div>
        </div>
      </div>
    </div>

    <!-- Rules -->
    <div class="card overflow-hidden">
      <div class="card-head card-pad pb-4">
        <div>
          <h2 class="section-title">入站规则</h2>
          <p class="caption">Ingress rules · 共 {{ rules.length }} 条</p>
        </div>
        <n-button size="small" secondary :loading="loading" :disabled="!selectedSecListID" @click="fetchRules">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新
        </n-button>
      </div>

      <div v-if="loading" class="divide-y divide-line border-t border-line">
        <div v-for="i in 3" :key="i" class="flex items-center gap-6 px-5 py-4">
          <n-skeleton text width="8%" />
          <n-skeleton text width="18%" />
          <n-skeleton text width="12%" />
          <n-skeleton text width="30%" />
        </div>
      </div>
      <EmptyState
        v-else-if="!selectedSecListID"
        title="先选择一个 VCN 和子网"
        description="没有 VCN？在「抢机任务」页面可以一键创建推荐网络。"
      />
      <EmptyState
        v-else-if="rules.length === 0"
        title="这个安全列表没有任何入站规则"
        description="外部网络目前无法连接任何端口。用上方按钮放通规则。"
      />
      <div v-else class="tbl-wrap border-t border-line">
        <table class="tbl">
          <thead>
            <tr>
              <th>协议</th>
              <th>来源</th>
              <th>目标端口</th>
              <th>说明</th>
              <th>状态跟踪</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, idx) in rules" :key="idx">
              <td><code class="mono rounded px-1.5 py-0.5 text-xs font-semibold" :class="protoClass(r.protocol)">{{ r.protocol }}</code></td>
              <td class="mono text-[13px] font-medium text-ink">{{ r.source }}</td>
              <td class="mono text-[13px] text-ink">{{ r.port_range || 'ALL' }}</td>
              <td class="text-ink-2">{{ r.description || '—' }}</td>
              <td class="text-xs text-ink-3">{{ r.is_stateless ? '无状态' : '有状态' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { NButton, NIcon, NSelect, NSkeleton, useMessage, useDialog } from 'naive-ui'
import { ShieldCheckmarkOutline, RefreshOutline } from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

const profileStore = useProfileStore()
const message = useMessage()
const dialog = useDialog()

const loading = ref(false)
const loadingNets = ref(false)
const operating = ref(false)
const selectedVCN = ref<string | null>(null)
const selectedSubnet = ref<string | null>(null)
const selectedSecListID = ref('')
const vcnOptions = ref<any[]>([])
const subnets = ref<any[]>([])
const rules = ref<any[]>([])

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))

const subnetOptions = computed(() =>
  subnets.value.map((s: any) => ({ label: `${s.display_name} (${s.cidr_block})`, value: s.ocid })),
)

const maskOCID = (ocid: string) => (ocid.length < 24 ? ocid : ocid.substring(0, 18) + '…' + ocid.substring(ocid.length - 8))

const protoClass = (p: string) => {
  const u = (p || '').toUpperCase()
  if (u === 'TCP') return 'bg-info-soft text-info'
  if (u === 'UDP') return 'bg-warn-soft text-warn'
  if (u === 'ICMP' || u.startsWith('ICMP')) return 'bg-surface-2 text-ink-2'
  return 'bg-brand-soft text-brand'
}

const resetTarget = () => {
  selectedVCN.value = null
  selectedSubnet.value = null
  selectedSecListID.value = ''
  subnets.value = []
  rules.value = []
}

const loadVCNs = async () => {
  if (!profileStore.activeProfileId) return
  loadingNets.value = true
  try {
    const res: any = await api.get(`/network/vcns?profile_id=${profileStore.activeProfileId}`)
    vcnOptions.value = (res.vcns || []).map((v: any) => ({ label: `${v.display_name} (${v.cidr_block})`, value: v.ocid }))
    if (vcnOptions.value.length > 0) {
      selectedVCN.value = vcnOptions.value[0].value
      await onVCNChange()
    }
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loadingNets.value = false
  }
}

const onVCNChange = async () => {
  selectedSubnet.value = null
  selectedSecListID.value = ''
  rules.value = []
  if (!selectedVCN.value || !profileStore.activeProfileId) return
  try {
    const res: any = await api.get(`/network/subnets?profile_id=${profileStore.activeProfileId}&vcn_id=${selectedVCN.value}`)
    subnets.value = res.subnets || []
    if (subnets.value.length > 0) {
      selectedSubnet.value = subnets.value[0].ocid
      onSubnetChange()
    }
  } catch (e: any) {
    message.error(e.message)
  }
}

const onSubnetChange = async () => {
  const s = subnets.value.find((x: any) => x.ocid === selectedSubnet.value)
  selectedSecListID.value = s?.security_list_id || ''
  rules.value = []
  if (selectedSecListID.value) await fetchRules()
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

const runFirewallAction = async (path: string) => {
  operating.value = true
  try {
    const res: any = await api.post(path, {
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

const handleAllowAll = () => {
  dialog.warning({
    title: '放通全部端口与协议',
    content: '将为 0.0.0.0/0 与 ::/0 添加全端口、全协议的入站放行规则。实例上所有监听的服务都会暴露到公网。',
    positiveText: '放通全部',
    negativeText: '取消',
    onPositiveClick: () => runFirewallAction('/network/allow-all'),
  })
}

const handleClearAll = () => {
  dialog.error({
    title: '清空所有入站规则',
    content: '清空后外部网络将无法连接任何端口，包括 SSH。确定继续？',
    positiveText: '清空',
    negativeText: '取消',
    onPositiveClick: () => runFirewallAction('/network/clear-all'),
  })
}

const handleAllowCloudflare = () => runFirewallAction('/network/allow-cloudflare')

watch(
  () => profileStore.activeProfileId,
  () => {
    resetTarget()
    loadVCNs()
  },
)

onMounted(() => {
  loadVCNs()
})
</script>
