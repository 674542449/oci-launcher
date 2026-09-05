<template>
  <div>
    <PageHeader title="实例" description="开关机、更换公网 IP、改配、附加 IPv6，以及保存在云端标签里的 Root 密码。">
      <template #actions>
        <n-button secondary :loading="loading" :disabled="!currentProfile" @click="fetchInstances">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新
        </n-button>
      </template>
    </PageHeader>

    <div class="card overflow-hidden">
      <!-- loading -->
      <div v-if="loading && instances.length === 0" class="divide-y divide-line">
        <div v-for="i in 3" :key="i" class="flex items-center gap-6 px-5 py-4">
          <n-skeleton text width="22%" />
          <n-skeleton text width="12%" />
          <n-skeleton text width="18%" />
          <n-skeleton text width="20%" />
        </div>
      </div>

      <!-- empty -->
      <EmptyState
        v-else-if="instances.length === 0"
        title="这个账号在主区域没有实例"
        description="创建实例后会显示在这里。"
      >
        <n-button type="primary" @click="$router.push('/launcher')">创建实例</n-button>
      </EmptyState>

      <!-- table -->
      <div v-else class="tbl-wrap">
        <table class="tbl">
          <thead>
            <tr>
              <th>实例</th>
              <th>状态</th>
              <th>规格</th>
              <th>网络</th>
              <th>Root 密码</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="inst in instances" :key="inst.ocid">
              <td class="min-w-[220px]">
                <div class="text-[14px] font-semibold text-ink">{{ inst.display_name }}</div>
                <div class="mono mt-0.5 text-xs text-ink-3" :title="inst.ocid">{{ maskOCID(inst.ocid) }}</div>
                <div class="mono mt-0.5 text-xs text-ink-3">{{ shortAD(inst.ad) }} · {{ formatDate(inst.time_created) }}</div>
              </td>
              <td><StatusPill :state="inst.state" /></td>
              <td class="whitespace-nowrap">
                <div class="mono text-[13px] text-ink">{{ inst.shape }}</div>
                <div class="mono mt-0.5 text-xs text-ink-3">{{ inst.ocpu }} OCPU · {{ inst.memory_in_gbs }} GB</div>
              </td>
              <td class="min-w-[220px]">
                <div v-if="inst.public_ip" class="flex items-center gap-2">
                  <span class="mono text-[13px] font-medium text-ink">{{ inst.public_ip }}</span>
                  <button type="button" class="txt-btn" @click="copyText(inst.public_ip, '公网 IP')">复制</button>
                  <button type="button" class="txt-btn-muted" :disabled="probing === inst.public_ip" @click="probeIP(inst.public_ip)">
                    {{ probing === inst.public_ip ? '探测中…' : '探测 22' }}
                  </button>
                </div>
                <div v-else class="text-xs text-ink-3">无公网 IPv4</div>
                <div v-if="inst.ipv6" class="mt-1 flex items-center gap-2">
                  <span class="mono max-w-[200px] truncate text-xs text-ink-2" :title="inst.ipv6">{{ inst.ipv6 }}</span>
                  <button type="button" class="txt-btn" @click="copyText(inst.ipv6, 'IPv6')">复制</button>
                </div>
              </td>
              <td class="whitespace-nowrap">
                <div v-if="inst.root_password" class="flex items-center gap-2">
                  <code class="mono rounded bg-surface-2 px-2 py-0.5 text-xs text-ink">{{ showPasswordMap[inst.ocid] ? inst.root_password : '••••••••••••' }}</code>
                  <button
                    type="button"
                    class="inline-flex h-7 w-7 items-center justify-center rounded text-ink-3 hover:bg-surface-2 hover:text-ink transition-colors"
                    :aria-label="showPasswordMap[inst.ocid] ? '隐藏密码' : '显示密码'"
                    @click="showPasswordMap[inst.ocid] = !showPasswordMap[inst.ocid]"
                  >
                    <n-icon size="16"><component :is="showPasswordMap[inst.ocid] ? EyeOffOutline : EyeOutline" /></n-icon>
                  </button>
                  <button type="button" class="txt-btn" @click="copyText(inst.root_password, 'Root 密码')">复制</button>
                </div>
                <div v-else class="text-xs text-ink-3">密钥登录 / 未设置</div>
              </td>
              <td class="text-right whitespace-nowrap">
                <div class="inline-flex items-center gap-1.5">
                  <n-button v-if="inst.state === 'STOPPED'" size="small" type="success" secondary :loading="acting === inst.ocid" @click="handleAction(inst, 'START')">开机</n-button>
                  <n-button v-if="inst.state === 'RUNNING'" size="small" type="warning" secondary :loading="acting === inst.ocid" @click="handleAction(inst, 'STOP')">关机</n-button>
                  <n-button size="small" secondary :disabled="!inst.ssh_command" @click="copyText(inst.ssh_command, 'SSH 命令')">SSH</n-button>
                  <n-dropdown trigger="click" :options="moreOptions(inst)" placement="bottom-end" @select="(key: string) => onMore(key, inst)">
                    <n-button size="small" secondary aria-label="更多操作">
                      <template #icon><n-icon><EllipsisHorizontal /></n-icon></template>
                    </n-button>
                  </n-dropdown>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Resize -->
    <n-modal v-model:show="showResizeModal" preset="card" title="实例改配" style="max-width: 460px" :bordered="false">
      <div v-if="selectedInst" class="space-y-5">
        <p class="caption">对 <b class="text-ink">{{ selectedInst.display_name }}</b> 调整 OCPU 与内存。运行中的实例会先停机，改配完成后再启动。</p>
        <div>
          <div class="mb-1.5 flex items-center justify-between">
            <span class="label mb-0">OCPU</span>
            <span class="mono text-sm font-semibold text-ink">{{ resizeOCPU }} 核</span>
          </div>
          <n-slider v-model:value="resizeOCPU" :min="1" :max="4" :step="1" :marks="{ 1: '1', 2: '2', 3: '3', 4: '4' }" />
        </div>
        <div>
          <div class="mb-1.5 flex items-center justify-between">
            <span class="label mb-0">内存</span>
            <span class="mono text-sm font-semibold text-ink">{{ resizeMemory }} GB</span>
          </div>
          <n-slider v-model:value="resizeMemory" :min="1" :max="24" :step="1" :marks="{ 6: '6', 12: '12', 18: '18', 24: '24' }" />
        </div>
        <p v-if="resizeMemory !== resizeOCPU * 6" class="caption">免费额度下每 OCPU 建议搭配 6 GB 内存（{{ resizeOCPU }} 核对应 {{ resizeOCPU * 6 }} GB）。</p>
        <div class="flex justify-end gap-2 pt-1">
          <n-button @click="showResizeModal = false">取消</n-button>
          <n-button type="primary" :loading="resizing" @click="submitResize">确认改配</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Tags -->
    <n-modal v-model:show="showEditTagsModal" preset="card" title="编辑 Root 密码标签" style="max-width: 460px" :bordered="false">
      <div v-if="selectedInst" class="space-y-4">
        <p class="caption">写入实例的云端自由标签 <code class="mono">root_password</code>。留空则删除该标签。这不会修改系统里的真实密码。</p>
        <n-form-item label="root_password" label-placement="top" :show-feedback="false">
          <n-input v-model:value="editRootPass" class="mono" placeholder="留空则删除" :input-props="{ autocomplete: 'off', spellcheck: 'false' }" />
        </n-form-item>
        <div class="flex justify-end gap-2 pt-1">
          <n-button @click="showEditTagsModal = false">取消</n-button>
          <n-button type="primary" :loading="updatingTags" @click="submitUpdateTags">保存到云端</n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, h } from 'vue'
import { NButton, NIcon, NModal, NSlider, NFormItem, NInput, NDropdown, NSkeleton, useMessage, useDialog } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import {
  RefreshOutline,
  EyeOutline,
  EyeOffOutline,
  EllipsisHorizontal,
  SwapHorizontalOutline,
  GlobeOutline,
  HardwareChipOutline,
  PricetagOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import StatusPill from '@/components/StatusPill.vue'
import EmptyState from '@/components/EmptyState.vue'

const profileStore = useProfileStore()
const message = useMessage()
const dialog = useDialog()

const loading = ref(false)
const instances = ref<any[]>([])
const showPasswordMap = ref<{ [key: string]: boolean }>({})
const acting = ref('')
const probing = ref('')

const showResizeModal = ref(false)
const selectedInst = ref<any>(null)
const resizeOCPU = ref(2)
const resizeMemory = ref(12)
const resizing = ref(false)

const showEditTagsModal = ref(false)
const editRootPass = ref('')
const updatingTags = ref(false)

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))

const maskOCID = (ocid?: string) => {
  if (!ocid || ocid.length < 20) return ocid || ''
  return ocid.substring(0, 10) + '…' + ocid.substring(ocid.length - 8)
}
const shortAD = (ad?: string) => (ad ? ad.replace(/^[^:]+:/, '') : '')
const formatDate = (t?: string) => {
  if (!t) return ''
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? t : d.toLocaleDateString('zh-CN')
}

const copyText = async (txt: string, what = '内容') => {
  if (!txt) return
  try {
    await navigator.clipboard.writeText(txt)
    message.success(`${what}已复制`)
  } catch {
    message.error('复制失败，请手动选择复制')
  }
}

const icon = (c: any) => () => h(NIcon, null, { default: () => h(c) })

const moreOptions = (inst: any): DropdownOption[] => [
  { label: '重启（软重启）', key: 'reboot', icon: icon(RefreshOutline), disabled: inst.state !== 'RUNNING' },
  { label: '更换公网 IP', key: 'rotate', icon: icon(SwapHorizontalOutline) },
  { label: '附加 IPv6', key: 'ipv6', icon: icon(GlobeOutline), disabled: !!inst.ipv6 },
  { label: '改配 OCPU / 内存', key: 'resize', icon: icon(HardwareChipOutline) },
  { label: '编辑 Root 密码标签', key: 'tags', icon: icon(PricetagOutline) },
  { type: 'divider', key: 'd1' },
  { label: '终止实例', key: 'terminate', icon: icon(TrashOutline), props: { style: 'color: var(--c-danger)' } },
]

const onMore = (key: string, inst: any) => {
  if (key === 'reboot') handleAction(inst, 'SOFTRESET')
  else if (key === 'rotate') handleRotateIP(inst)
  else if (key === 'ipv6') handleAttachIPv6(inst)
  else if (key === 'resize') openResizeModal(inst)
  else if (key === 'tags') openEditTagsModal(inst)
  else if (key === 'terminate') confirmTerminate(inst)
}

const fetchInstances = async () => {
  if (!profileStore.activeProfileId) return
  loading.value = true
  try {
    const res: any = await api.get(`/instances?profile_id=${profileStore.activeProfileId}`)
    instances.value = res.instances || []
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loading.value = false
  }
}

const handleAction = async (inst: any, action: string) => {
  acting.value = inst.ocid
  try {
    await api.post('/instances/action', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: inst.ocid,
      action,
    })
    message.success(`已下发 ${action} 指令`)
    setTimeout(fetchInstances, 2000)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    acting.value = ''
  }
}

const handleRotateIP = (inst: any) => {
  dialog.warning({
    title: '更换公网 IP',
    content: `将释放 ${inst.display_name} 当前的公网 IP ${inst.public_ip || ''}，并申请一个新的临时公网 IP。旧 IP 无法找回。`,
    positiveText: '更换',
    negativeText: '取消',
    onPositiveClick: async () => {
      const loadingMsg = message.loading('正在解绑旧 IP 并申请新 IP…', { duration: 0 })
      try {
        const res: any = await api.post('/instances/rotate-ip', {
          profile_id: profileStore.activeProfileId,
          region: currentProfile.value?.region,
          ocid: inst.ocid,
        })
        message.success(`新公网 IP：${res.new_ip}`)
        await fetchInstances()
      } catch (e: any) {
        message.error(e.message)
      } finally {
        loadingMsg.destroy()
      }
    },
  })
}

const probeIP = async (ip: string) => {
  probing.value = ip
  try {
    const res: any = await api.post('/instances/probe-ip', { ip, port: 22 })
    if (res.reachable) message.success(`${ip}:22 可连通`)
    else message.warning(`${ip}:22 无响应，系统可能还在启动，或防火墙未放行`)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    probing.value = ''
  }
}

const handleAttachIPv6 = async (inst: any) => {
  const loadingMsg = message.loading('正在分配 IPv6 地址…', { duration: 0 })
  try {
    const res: any = await api.post('/instances/attach-ipv6', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: inst.ocid,
    })
    message.success(`IPv6 已附加：${res.ipv6}`)
    await fetchInstances()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loadingMsg.destroy()
  }
}

const confirmTerminate = (inst: any) => {
  dialog.error({
    title: '终止并销毁实例',
    content: `确定要终止 ${inst.display_name} 吗？实例连同引导卷和数据都会被删除，无法恢复。`,
    positiveText: '终止实例',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await api.post('/instances/terminate', {
          profile_id: profileStore.activeProfileId,
          region: currentProfile.value?.region,
          ocid: inst.ocid,
        })
        message.success('终止指令已执行')
        setTimeout(fetchInstances, 2000)
      } catch (e: any) {
        message.error(e.message)
      }
    },
  })
}

const openResizeModal = (inst: any) => {
  selectedInst.value = inst
  resizeOCPU.value = inst.ocpu || 2
  resizeMemory.value = inst.memory_in_gbs || 12
  showResizeModal.value = true
}

const submitResize = async () => {
  if (!selectedInst.value) return
  resizing.value = true
  try {
    await api.post('/instances/resize', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: selectedInst.value.ocid,
      new_ocpu: resizeOCPU.value,
      new_memory: resizeMemory.value,
    })
    message.success('改配指令已提交')
    showResizeModal.value = false
    setTimeout(fetchInstances, 3000)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    resizing.value = false
  }
}

const openEditTagsModal = (inst: any) => {
  selectedInst.value = inst
  editRootPass.value = inst.root_password || ''
  showEditTagsModal.value = true
}

const submitUpdateTags = async () => {
  if (!selectedInst.value) return
  updatingTags.value = true
  try {
    const tags = { ...(selectedInst.value.freeform_tags || {}) }
    if (editRootPass.value) tags['root_password'] = editRootPass.value
    else delete tags['root_password']
    await api.post('/instances/update-tags', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: selectedInst.value.ocid,
      tags,
    })
    message.success('云端标签已更新')
    showEditTagsModal.value = false
    await fetchInstances()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    updatingTags.value = false
  }
}

watch(
  () => profileStore.activeProfileId,
  () => {
    instances.value = []
    fetchInstances()
  },
)

onMounted(() => {
  fetchInstances()
})
</script>
