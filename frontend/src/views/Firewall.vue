<template>
  <div>
    <PageHeader title="防火墙" description="查看并修改子网安全列表（Security List）的入站规则。">
      <template #actions>
        <n-button secondary :loading="operating" :disabled="!selectedSecListID" @click="handleAllowCloudflare">
          <template #icon><n-icon><ShieldCheckmarkOutline /></n-icon></template>
          放通 Cloudflare 80/443
        </n-button>
        <n-button secondary type="success" :loading="operating" :disabled="!selectedSecListID" @click="handleAllowAll">放通全部</n-button>
        <n-button secondary type="error" :loading="operating" :disabled="!selectedSecListID" @click="handleClearAll">清空入站规则</n-button>
        <n-button type="primary" :disabled="!selectedSecListID" @click="openAddModal">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          添加规则
        </n-button>
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
          <p class="caption">Ingress rules · 共 {{ rules.length }} 条 · 上限 200 条</p>
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
        description="没有 VCN？在「创建实例」页面可以一键创建推荐网络。"
      />
      <EmptyState
        v-else-if="rules.length === 0"
        title="这个安全列表没有任何入站规则"
        description="外部网络目前无法连接任何端口。点右上角「添加规则」放行需要的端口。"
      >
        <n-button type="primary" @click="openAddModal">添加规则</n-button>
      </EmptyState>
      <div v-else class="tbl-wrap border-t border-line">
        <table class="tbl">
          <thead>
            <tr>
              <th>协议</th>
              <th>来源</th>
              <th>目标端口</th>
              <th>说明</th>
              <th>状态跟踪</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in rules" :key="r.key || r.protocol + r.source + r.port_range">
              <td><code class="mono rounded px-1.5 py-0.5 text-xs font-semibold" :class="protoClass(r.protocol)">{{ r.protocol }}</code></td>
              <td class="mono text-[13px] font-medium text-ink">{{ r.source }}</td>
              <td class="mono text-[13px] text-ink">{{ r.port_range || 'ALL' }}</td>
              <td class="text-ink-2">{{ r.description || '—' }}</td>
              <td class="text-xs text-ink-3">{{ r.is_stateless ? '无状态' : '有状态' }}</td>
              <td class="text-right whitespace-nowrap">
                <n-button size="small" quaternary type="error" :disabled="!r.key" :loading="deletingKey === r.key" @click="confirmDeleteRule(r)">删除</n-button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Add rule -->
    <n-modal v-model:show="showAddModal" preset="card" title="添加入站规则" style="max-width: 520px" :bordered="false">
      <n-form label-placement="top" :show-feedback="false" @submit.prevent="submitAddRule">
        <div class="space-y-4">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <n-form-item label="协议">
              <n-select v-model:value="addForm.protocol" :options="protocolOptions" />
            </n-form-item>
            <n-form-item label="来源 IP / CIDR">
              <n-input v-model:value="addForm.source" class="mono" placeholder="0.0.0.0/0" :input-props="{ spellcheck: 'false' }" />
            </n-form-item>
          </div>
          <p class="caption -mt-2">来源填单个 IP 会自动补成 /32；IPv6 全部为 <code class="mono">::/0</code>，IPv4 全部为 <code class="mono">0.0.0.0/0</code>。</p>

          <n-form-item v-if="addForm.protocol === 'tcp' || addForm.protocol === 'udp'" label="目标端口">
            <n-input v-model:value="addForm.ports" class="mono" placeholder="单个端口 22，或范围 8000-8100，留空表示全部端口" :input-props="{ spellcheck: 'false' }" />
          </n-form-item>

          <n-form-item label="说明（可选）">
            <n-input v-model:value="addForm.description" placeholder="例如：SSH、Web、WireGuard" maxlength="255" />
          </n-form-item>

          <n-checkbox v-model:checked="addForm.is_stateless">无状态规则（一般不需要勾选）</n-checkbox>

          <div v-if="addPreview" class="rounded-lg border border-line bg-surface-2 px-3 py-2 text-xs text-ink-2">
            将添加：<span class="mono text-ink">{{ addPreview }}</span>
          </div>

          <div class="flex justify-end gap-2 border-t border-line pt-4">
            <n-button @click="showAddModal = false">取消</n-button>
            <n-button type="primary" attr-type="submit" :loading="adding">添加</n-button>
          </div>
        </div>
      </n-form>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { NButton, NIcon, NSelect, NSkeleton, NModal, NForm, NFormItem, NInput, NCheckbox, useMessage, useDialog } from 'naive-ui'
import { ShieldCheckmarkOutline, RefreshOutline, AddOutline } from '@vicons/ionicons5'
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
const deletingKey = ref('')
const selectedVCN = ref<string | null>(null)
const selectedSubnet = ref<string | null>(null)
const selectedSecListID = ref('')
const vcnOptions = ref<any[]>([])
const subnets = ref<any[]>([])
const rules = ref<any[]>([])

const showAddModal = ref(false)
const adding = ref(false)
const addForm = ref({ protocol: 'tcp', source: '0.0.0.0/0', ports: '', description: '', is_stateless: false })

const protocolOptions = [
  { label: 'TCP', value: 'tcp' },
  { label: 'UDP', value: 'udp' },
  { label: 'ICMP（ping 等）', value: 'icmp' },
  { label: '全部协议', value: 'all' },
]

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))

const subnetOptions = computed(() =>
  subnets.value.map((s: any) => ({ label: `${s.display_name} (${s.cidr_block})`, value: s.ocid })),
)

const maskOCID = (ocid: string) => (ocid.length < 24 ? ocid : ocid.substring(0, 18) + '…' + ocid.substring(ocid.length - 8))

const protoClass = (p: string) => {
  const u = (p || '').toUpperCase()
  if (u === 'TCP') return 'bg-info-soft text-info'
  if (u === 'UDP') return 'bg-warn-soft text-warn'
  if (u.startsWith('ICMP')) return 'bg-surface-2 text-ink-2'
  return 'bg-brand-soft text-brand'
}

// "22" -> [22,22]; "8000-8100" -> [8000,8100]; "" -> [0,0] (all ports)
const parsePorts = (s: string): [number, number] | null => {
  const t = s.trim()
  if (!t) return [0, 0]
  const m = t.match(/^(\d{1,5})(?:\s*-\s*(\d{1,5}))?$/)
  if (!m) return null
  const lo = Number(m[1])
  const hi = m[2] ? Number(m[2]) : lo
  if (lo < 1 || hi > 65535 || lo > hi) return null
  return [lo, hi]
}

const addPreview = computed(() => {
  const f = addForm.value
  const proto = f.protocol.toUpperCase()
  const src = f.source.trim() || '?'
  if (f.protocol === 'tcp' || f.protocol === 'udp') {
    const p = parsePorts(f.ports)
    const ports = p === null ? '端口格式错误' : p[0] === 0 ? '全部端口' : p[0] === p[1] ? `端口 ${p[0]}` : `端口 ${p[0]}-${p[1]}`
    return `${proto} 来自 ${src} → ${ports}`
  }
  return `${proto} 来自 ${src}`
})

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

const openAddModal = () => {
  addForm.value = { protocol: 'tcp', source: '0.0.0.0/0', ports: '', description: '', is_stateless: false }
  showAddModal.value = true
}

const submitAddRule = async () => {
  const f = addForm.value
  if (!f.source.trim()) {
    message.warning('请填写来源 IP 或 CIDR')
    return
  }
  let portMin = 0
  let portMax = 0
  if (f.protocol === 'tcp' || f.protocol === 'udp') {
    const p = parsePorts(f.ports)
    if (p === null) {
      message.warning('端口格式不对：填单个端口如 22，或范围如 8000-8100')
      return
    }
    ;[portMin, portMax] = p
  }
  adding.value = true
  try {
    const res: any = await api.post('/network/security-rules/add', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      security_list_id: selectedSecListID.value,
      protocol: f.protocol,
      source: f.source.trim(),
      port_min: portMin,
      port_max: portMax,
      description: f.description.trim(),
      is_stateless: f.is_stateless,
    })
    message.success(res.message || '规则已添加')
    showAddModal.value = false
    await fetchRules()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    adding.value = false
  }
}

const confirmDeleteRule = (r: any) => {
  dialog.warning({
    title: '删除这条入站规则',
    content: `${r.protocol} 来自 ${r.source}，端口 ${r.port_range || 'ALL'}${r.description ? '（' + r.description + '）' : ''}。删除后对应端口将无法从外部访问。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      deletingKey.value = r.key
      try {
        const res: any = await api.post('/network/security-rules/delete', {
          profile_id: profileStore.activeProfileId,
          region: currentProfile.value?.region,
          security_list_id: selectedSecListID.value,
          key: r.key,
        })
        message.success(res.message || '规则已删除')
        await fetchRules()
      } catch (e: any) {
        message.error(e.message)
      } finally {
        deletingKey.value = ''
      }
    },
  })
}

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
