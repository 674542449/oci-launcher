<template>
  <div>
    <!-- loading -->
    <div v-if="loading" class="space-y-3 py-2">
      <n-skeleton text width="55%" />
      <n-skeleton text width="90%" />
      <n-skeleton height="96px" :sharp="false" />
    </div>

    <!-- error -->
    <div v-else-if="error" class="notice notice-danger">
      <n-icon size="18" class="mt-0.5 shrink-0"><WarningOutline /></n-icon>
      <span>{{ error }}</span>
    </div>

    <!-- not enabled yet -->
    <div v-else-if="fw && !fw.nsg" class="space-y-4">
      <div class="notice notice-info">
        <n-icon size="18" class="mt-0.5 shrink-0"><ShieldCheckmarkOutline /></n-icon>
        <span>
          这台实例还没有专属防火墙。启用后会创建一个只属于它的网络安全组（NSG），里面的规则只对这台实例生效；子网安全列表的规则仍然叠加生效（任一放行即放行）。
        </span>
      </div>
      <div class="flex justify-end">
        <n-button type="primary" :loading="enabling" @click="enable">启用专属防火墙</n-button>
      </div>
    </div>

    <!-- enabled -->
    <div v-else-if="fw && fw.nsg" class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="min-w-0 text-[13px] leading-5 text-ink-2">
          网络安全组 <span class="mono text-ink">{{ fw.nsg.name || maskOCID(fw.nsg.id) }}</span>
          <span v-if="fw.other_nsg_count" class="caption ml-2">另有 {{ fw.other_nsg_count }} 个安全组也挂在这台实例上</span>
        </div>
        <div class="flex items-center gap-2">
          <n-button size="small" secondary @click="openAdd">
            <template #icon><n-icon><AddOutline /></n-icon></template>
            添加规则
          </n-button>
          <n-dropdown trigger="click" :options="quickOptions" placement="bottom-end" @select="onQuick">
            <n-button size="small" secondary :loading="quickBusy">
              快捷操作
              <template #icon><n-icon><ChevronDownOutline /></n-icon></template>
            </n-button>
          </n-dropdown>
          <n-button size="small" secondary type="error" :loading="removing" @click="confirmRemove">移除专属防火墙</n-button>
        </div>
      </div>

      <div v-if="!fw.rules.length" class="rounded-xl border border-dashed border-line px-4 py-6 text-center">
        <div class="text-[13px] font-medium text-ink">还没有专属规则</div>
        <p class="caption mt-1">现在只有子网安全列表在生效（默认放行 22 和 ICMP）。添加的规则只对这台实例生效。</p>
      </div>
      <div v-else class="tbl-wrap rounded-xl border border-line">
        <table class="tbl">
          <thead>
            <tr>
              <th>协议</th>
              <th>来源</th>
              <th>目标端口</th>
              <th>说明</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in fw.rules" :key="r.id">
              <td><code class="mono rounded px-1.5 py-0.5 text-xs font-semibold" :class="protoClass(r.protocol)">{{ r.protocol }}</code></td>
              <td class="mono text-[13px] font-medium text-ink">{{ r.source }}</td>
              <td class="mono text-[13px] text-ink">{{ r.port_range || 'ALL' }}</td>
              <td class="max-w-[200px] truncate text-ink-2" :title="r.description">{{ r.description || '—' }}</td>
              <td class="text-right">
                <n-button size="small" secondary type="error" :loading="deletingId === r.id" @click="confirmDelete(r)">删除</n-button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p class="caption">OCI 的判定是"子网安全列表或专属防火墙任一放行即放行"。子网安全列表保持最小，这台实例需要的端口放在这里。</p>
    </div>

    <!-- add rule -->
    <n-modal v-model:show="showAdd" preset="card" title="添加入站规则" style="width: 92vw; max-width: 520px" :bordered="false">
      <n-form label-placement="top" :show-feedback="false" @submit.prevent="submitAdd">
        <div class="space-y-4">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <n-form-item label="协议">
              <n-select v-model:value="addForm.protocol" :options="protocolOptions" />
            </n-form-item>
            <n-form-item label="来源 IP / CIDR">
              <n-input v-model:value="addForm.source" class="mono" placeholder="0.0.0.0/0" :input-props="{ spellcheck: 'false' }" />
            </n-form-item>
          </div>
          <p class="caption -mt-2">来源填单个 IP 会自动补成 /32；IPv4 全部为 <code class="mono">0.0.0.0/0</code>，IPv6 全部为 <code class="mono">::/0</code>。</p>

          <n-form-item v-if="addForm.protocol === 'tcp' || addForm.protocol === 'udp'" label="目标端口">
            <n-input v-model:value="addForm.ports" class="mono" placeholder="单个端口 22，或范围 8000-8100，留空表示全部端口" :input-props="{ spellcheck: 'false' }" />
          </n-form-item>

          <n-form-item label="说明（可选）">
            <n-input v-model:value="addForm.description" placeholder="例如：Web、WireGuard" maxlength="255" />
          </n-form-item>

          <div v-if="addPreview" class="rounded-lg border border-line bg-surface-2 px-3 py-2 text-xs text-ink-2">
            将添加：<span class="mono text-ink">{{ addPreview }}</span>
          </div>

          <div class="flex justify-end gap-2 border-t border-line pt-4">
            <n-button @click="showAdd = false">取消</n-button>
            <n-button type="primary" attr-type="submit" :loading="adding">添加</n-button>
          </div>
        </div>
      </n-form>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { NButton, NIcon, NSkeleton, NModal, NForm, NFormItem, NInput, NSelect, NDropdown, useMessage, useDialog } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import { AddOutline, ShieldCheckmarkOutline, WarningOutline, ChevronDownOutline } from '@vicons/ionicons5'
import { api } from '@/api/client'

const props = defineProps<{
  profileId: number
  region: string
  instance: any
}>()

const message = useMessage()
const dialog = useDialog()

const loading = ref(false)
const error = ref('')
const fw = ref<any>(null)
const enabling = ref(false)
const removing = ref(false)
const deletingId = ref('')

const showAdd = ref(false)
const adding = ref(false)
const addForm = ref({ protocol: 'tcp', source: '0.0.0.0/0', ports: '', description: '' })

const protocolOptions = [
  { label: 'TCP', value: 'tcp' },
  { label: 'UDP', value: 'udp' },
  { label: 'ICMP（ping 等）', value: 'icmp' },
  { label: '全部协议', value: 'all' },
]

const maskOCID = (ocid: string) => (!ocid || ocid.length < 24 ? ocid : ocid.substring(0, 18) + '…' + ocid.substring(ocid.length - 8))

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
  if (f.protocol !== 'tcp' && f.protocol !== 'udp') return `${proto} 来自 ${src}`
  const p = parsePorts(f.ports)
  const ports = p === null ? '端口格式错误' : p[0] === 0 ? '全部端口' : p[0] === p[1] ? `端口 ${p[0]}` : `端口 ${p[0]}-${p[1]}`
  return `${proto} 来自 ${src} → ${ports}`
})

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const res: any = await api.get(`/instances/firewall?profile_id=${props.profileId}&ocid=${encodeURIComponent(props.instance.ocid)}`)
    fw.value = res.firewall
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

const enable = async () => {
  enabling.value = true
  try {
    const res: any = await api.post('/instances/firewall/enable', { profile_id: props.profileId, region: props.region, ocid: props.instance.ocid })
    fw.value = res.firewall
    message.success(res.message || '已启用')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    enabling.value = false
  }
}

const openAdd = () => {
  addForm.value = { protocol: 'tcp', source: '0.0.0.0/0', ports: '', description: '' }
  showAdd.value = true
}

const submitAdd = async () => {
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
      message.warning('端口格式不正确：填单个端口如 22，或范围如 8000-8100')
      return
    }
    ;[portMin, portMax] = p
  }
  adding.value = true
  try {
    const res: any = await api.post('/instances/firewall/rules/add', {
      profile_id: props.profileId,
      region: props.region,
      nsg_id: fw.value.nsg.id,
      protocol: f.protocol,
      source: f.source.trim(),
      port_min: portMin,
      port_max: portMax,
      description: f.description.trim(),
    })
    message.success(res.message || '规则已添加')
    showAdd.value = false
    await load()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    adding.value = false
  }
}

const confirmDelete = (r: any) => {
  dialog.warning({
    title: '删除规则',
    content: `${r.protocol} 来自 ${r.source}，端口 ${r.port_range || 'ALL'}${r.description ? '（' + r.description + '）' : ''}。删除后立即生效。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      deletingId.value = r.id
      try {
        await api.post('/instances/firewall/rules/delete', { profile_id: props.profileId, region: props.region, nsg_id: fw.value.nsg.id, rule_id: r.id })
        message.success('规则已删除')
        await load()
      } catch (e: any) {
        message.error(e.message)
      } finally {
        deletingId.value = ''
      }
    },
  })
}

// ---------- shortcuts (moved here from the subnet list) ----------
const quickBusy = ref(false)
const quickOptions: DropdownOption[] = [
  { label: '放通 Cloudflare CDN 的 80 / 443', key: 'cf' },
  { label: '放通全部端口与协议', key: 'all' },
  { type: 'divider', key: 'd' },
  { label: '清空所有规则', key: 'clear', props: { style: 'color: var(--c-danger)' } },
]
const runQuick = async (path: string) => {
  quickBusy.value = true
  try {
    const res: any = await api.post(path, { profile_id: props.profileId, region: props.region, nsg_id: fw.value.nsg.id })
    message.success(res.message || '已完成')
    await load()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    quickBusy.value = false
  }
}
const onQuick = (key: string) => {
  if (key === 'cf') {
    runQuick('/instances/firewall/allow-cloudflare')
  } else if (key === 'all') {
    dialog.warning({
      title: '放通全部端口与协议',
      content: `将为 ${props.instance.display_name} 添加 0.0.0.0/0 与 ::/0 的全端口、全协议入站规则，它上面所有监听的服务都会暴露到公网。`,
      positiveText: '放通全部',
      negativeText: '取消',
      onPositiveClick: () => runQuick('/instances/firewall/allow-all'),
    })
  } else if (key === 'clear') {
    dialog.error({
      title: '清空所有规则',
      content: `清空后 ${props.instance.display_name} 只剩子网安全列表的规则；如果安全列表已是最小规则，外部将无法连接任何端口，包括 SSH。`,
      positiveText: '清空',
      negativeText: '取消',
      onPositiveClick: () => runQuick('/instances/firewall/clear'),
    })
  }
}

const confirmRemove = () => {
  dialog.warning({
    title: '移除专属防火墙',
    content: `将从 ${props.instance.display_name} 解绑并删除这个网络安全组，里面的 ${fw.value.rules.length} 条规则一起失效，之后只剩子网安全列表的规则。`,
    positiveText: '移除',
    negativeText: '取消',
    onPositiveClick: async () => {
      removing.value = true
      try {
        const res: any = await api.post('/instances/firewall/disable', { profile_id: props.profileId, region: props.region, ocid: props.instance.ocid, nsg_id: fw.value.nsg.id })
        message.success(res.message || '已移除')
        await load()
      } catch (e: any) {
        message.error(e.message)
      } finally {
        removing.value = false
      }
    },
  })
}

onMounted(load)
</script>
