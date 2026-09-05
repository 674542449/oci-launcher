<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h2 class="text-xl font-bold text-gray-900">实例与网络管理</h2>
        <p class="text-xs text-gray-500">全生命周期管理 · 一键更换公网 IP · 在线改配 · 附加 IPv6 · 密码标签查看与修改</p>
      </div>
      <div class="flex items-center space-x-3">
        <n-button :loading="loading" type="primary" secondary @click="fetchInstances">
          🔄 刷新实例列表
        </n-button>
      </div>
    </div>

    <!-- Idle Reclaim Warning Card -->
    <div class="p-4 bg-amber-50 border border-amber-200 rounded-xl text-xs text-amber-800 flex items-center space-x-2">
      <span>🛡️</span>
      <span><b>官方闲置回收政策提醒:</b> 连续 7 天内 CPU、网络与内存使用率均低于 20% 的 Always Free 实例可能会被甲骨文系统判定为闲置并回收，请合理规划负载。</span>
    </div>

    <!-- Instances Table -->
    <div class="bg-white rounded-2xl border border-gray-200 shadow-sm overflow-hidden">
      <div v-if="loading && instances.length === 0" class="p-12 text-center text-gray-400">
        正在拉取实例与网络信息...
      </div>
      <div v-else-if="instances.length === 0" class="p-12 text-center text-gray-400">
        当前账号在主区域暂无存活实例
      </div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-200 text-left text-xs">
          <thead class="bg-gray-50 text-gray-500 font-medium">
            <tr>
              <th class="px-6 py-3">实例名称 / OCID</th>
              <th class="px-6 py-3">运行状态</th>
              <th class="px-6 py-3">硬件规格</th>
              <th class="px-6 py-3">网络地址 (IPv4 / IPv6)</th>
              <th class="px-6 py-3">Root 密码 (云端标签)</th>
              <th class="px-6 py-3 text-right">操作管理</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 text-gray-700">
            <tr v-for="inst in instances" :key="inst.ocid" class="hover:bg-gray-50/80 transition-colors">
              <!-- Name & OCID -->
              <td class="px-6 py-4">
                <div class="font-bold text-gray-900 text-sm">{{ inst.display_name }}</div>
                <div class="text-gray-400 font-mono text-[10px]">{{ maskOCID(inst.ocid) }}</div>
                <div class="text-[10px] text-gray-400">{{ inst.ad }} · {{ inst.time_created }}</div>
              </td>

              <!-- State -->
              <td class="px-6 py-4">
                <span
                  class="px-2.5 py-1 rounded-full text-xs font-semibold"
                  :class="inst.state === 'RUNNING' ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : (inst.state === 'STOPPED' ? 'bg-gray-100 text-gray-700' : 'bg-amber-50 text-amber-700 animate-pulse')"
                >
                  {{ inst.state }}
                </span>
              </td>

              <!-- Specs -->
              <td class="px-6 py-4">
                <div class="font-medium text-gray-900">{{ inst.shape }}</div>
                <div class="text-gray-500 text-[11px]">{{ inst.ocpu }} OCPU / {{ inst.memory_in_gbs }} GB 内存</div>
              </td>

              <!-- IPs -->
              <td class="px-6 py-4 space-y-1 font-mono">
                <div v-if="inst.public_ip" class="flex items-center space-x-1">
                  <span class="text-gray-900 font-bold">{{ inst.public_ip }}</span>
                  <button @click="copyText(inst.public_ip)" class="text-blue-500 hover:underline text-[10px]">复制</button>
                  <button @click="probeIP(inst.public_ip)" class="text-gray-400 hover:text-gray-600 text-[10px]">探测</button>
                </div>
                <div v-else class="text-gray-400 text-[11px]">无公网 IPv4</div>

                <div v-if="inst.ipv6" class="text-[11px] text-purple-600 truncate max-w-[180px]" :title="inst.ipv6">
                  IPv6: {{ inst.ipv6 }}
                </div>
              </td>

              <!-- Root Password Tag -->
              <td class="px-6 py-4">
                <div v-if="inst.root_password" class="flex items-center space-x-1.5 font-mono">
                  <span class="bg-gray-100 px-2 py-0.5 rounded text-gray-800 text-xs">
                    {{ showPasswordMap[inst.ocid] ? inst.root_password : '••••••••••••' }}
                  </span>
                  <button
                    @click="showPasswordMap[inst.ocid] = !showPasswordMap[inst.ocid]"
                    class="text-gray-400 hover:text-gray-600 text-xs"
                  >
                    {{ showPasswordMap[inst.ocid] ? '🙈' : '👁️' }}
                  </button>
                  <button @click="copyText(inst.root_password)" class="text-blue-500 hover:underline text-[10px]">复制</button>
                </div>
                <div v-else class="text-gray-400 text-[11px]">
                  未设置或为密钥模式
                </div>
              </td>

              <!-- Actions -->
              <td class="px-6 py-4 text-right space-x-1.5">
                <!-- Power Actions -->
                <n-button
                  v-if="inst.state === 'STOPPED'"
                  size="tiny"
                  type="success"
                  secondary
                  @click="handleAction(inst, 'START')"
                >
                  开机
                </n-button>
                <n-button
                  v-if="inst.state === 'RUNNING'"
                  size="tiny"
                  type="warning"
                  secondary
                  @click="handleAction(inst, 'STOP')"
                >
                  关机
                </n-button>
                <n-button
                  v-if="inst.state === 'RUNNING'"
                  size="tiny"
                  secondary
                  @click="handleAction(inst, 'SOFTRESET')"
                >
                  重启
                </n-button>

                <!-- Change IP -->
                <n-button size="tiny" secondary @click="handleRotateIP(inst)">
                  刷IP
                </n-button>

                <!-- Attach IPv6 -->
                <n-button v-if="!inst.ipv6" size="tiny" secondary @click="handleAttachIPv6(inst)">
                  +IPv6
                </n-button>

                <!-- Resize -->
                <n-button size="tiny" secondary @click="openResizeModal(inst)">
                  改配
                </n-button>

                <!-- Edit Tags -->
                <n-button size="tiny" secondary @click="openEditTagsModal(inst)">
                  编辑标签
                </n-button>

                <!-- Copy SSH -->
                <n-button size="tiny" type="info" secondary @click="copyText(inst.ssh_command)">
                  SSH
                </n-button>

                <!-- Terminate -->
                <n-button size="tiny" type="error" secondary @click="confirmTerminate(inst)">
                  终止
                </n-button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Resize Modal -->
    <n-modal v-model:show="showResizeModal" preset="card" title="实例改配 (Resize)" style="max-width: 450px;">
      <div v-if="selectedInst" class="space-y-4">
        <p class="text-xs text-gray-500">将对实例 {{ selectedInst.display_name }} 执行改配（若处于运行中会自动停机后生效）</p>
        <div>
          <span class="text-xs font-medium block mb-1">目标 OCPU 核心数: {{ resizeOCPU }} 核</span>
          <n-slider v-model:value="resizeOCPU" :min="1" :max="4" :step="1" />
        </div>
        <div>
          <span class="text-xs font-medium block mb-1">目标内存: {{ resizeMemory }} GB</span>
          <n-slider v-model:value="resizeMemory" :min="1" :max="24" :step="1" />
        </div>
        <div class="flex justify-end space-x-2 pt-2">
          <n-button @click="showResizeModal = false">取消</n-button>
          <n-button type="primary" :loading="resizing" @click="submitResize">确认改配</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Edit Tags Modal -->
    <n-modal v-model:show="showEditTagsModal" preset="card" title="修改实例云端标签 (Freeform Tags)" style="max-width: 450px;">
      <div v-if="selectedInst" class="space-y-4">
        <p class="text-xs text-gray-500">直接同步更新至 Oracle Cloud 云端实例自由标签，支持修改 Root 密码：</p>
        <n-form-item label="root_password 标签">
          <n-input v-model:value="editRootPass" placeholder="留空则删除该标签" />
        </n-form-item>
        <div class="flex justify-end space-x-2 pt-2">
          <n-button @click="showEditTagsModal = false">取消</n-button>
          <n-button type="primary" :loading="updatingTags" @click="submitUpdateTags">同步保存到云端</n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { useMessage, useDialog } from 'naive-ui'

const profileStore = useProfileStore()
const message = useMessage()
const dialog = useDialog()

const loading = ref(false)
const instances = ref<any[]>([])
const showPasswordMap = ref<{ [key: string]: boolean }>({})

const showResizeModal = ref(false)
const selectedInst = ref<any>(null)
const resizeOCPU = ref(2)
const resizeMemory = ref(12)
const resizing = ref(false)

const showEditTagsModal = ref(false)
const editRootPass = ref('')
const updatingTags = ref(false)

const currentProfile = computed(() => {
  return profileStore.profiles.find(p => p.id === profileStore.activeProfileId)
})

const maskOCID = (ocid?: string) => {
  if (!ocid || ocid.length < 20) return ocid || ''
  return ocid.substring(0, 10) + '...' + ocid.substring(ocid.length - 8)
}

const copyText = (txt: string) => {
  if (!txt) return
  navigator.clipboard.writeText(txt)
  message.success('已复制到剪贴板')
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
  try {
    await api.post('/instances/action', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: inst.ocid,
      action: action,
    })
    message.success(`操作 [${action}] 指令已下发`)
    setTimeout(fetchInstances, 2000)
  } catch (e: any) {
    message.error(e.message)
  }
}

const handleRotateIP = async (inst: any) => {
  if (!confirm(`确定要为实例 ${inst.display_name} 更换新的公网 IP 吗？当前 IP 将被释放。`)) return
  try {
    message.loading('正在云端解绑旧 IP 并申请全新公网 IPv4...')
    const res: any = await api.post('/instances/rotate-ip', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: inst.ocid,
    })
    message.success(`公网 IP 更换成功: ${res.new_ip}`)
    await fetchInstances()
  } catch (e: any) {
    message.error(e.message)
  }
}

const probeIP = async (ip: string) => {
  try {
    const res: any = await api.post('/instances/probe-ip', { ip, port: 22 })
    if (res.reachable) {
      message.success(`IP [${ip}:22] 连通测试通过！SSH 端口正常`)
    } else {
      message.warning(`IP [${ip}:22] 端口无响应，可能系统正在启动或内部防火墙未放通`)
    }
  } catch (e: any) {
    message.error(e.message)
  }
}

const handleAttachIPv6 = async (inst: any) => {
  try {
    message.loading('正在为实例附加分配全新 IPv6 地址...')
    const res: any = await api.post('/instances/attach-ipv6', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: inst.ocid,
    })
    message.success(`IPv6 附加成功: ${res.ipv6}`)
    await fetchInstances()
  } catch (e: any) {
    message.error(e.message)
  }
}

const confirmTerminate = (inst: any) => {
  dialog.warning({
    title: '二次安全确认：终止（销毁）实例',
    content: `您确定要彻底终止（删机）实例 [${inst.display_name}] 吗？引导卷及数据将被彻底销毁，该操作不可恢复！`,
    positiveText: '确认终止',
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
    message.success('改配指令已提交！')
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
    if (editRootPass.value) {
      tags['root_password'] = editRootPass.value
    } else {
      delete tags['root_password']
    }
    await api.post('/instances/update-tags', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: selectedInst.value.ocid,
      tags: tags,
    })
    message.success('云端自由标签已成功同步更新！')
    showEditTagsModal.value = false
    await fetchInstances()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    updatingTags.value = false
  }
}

watch(() => profileStore.activeProfileId, () => {
  fetchInstances()
})

onMounted(() => {
  fetchInstances()
})
</script>
