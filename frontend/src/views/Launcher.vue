<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h2 class="text-xl font-bold text-gray-900">抢机开机控制台</h2>
        <p class="text-xs text-gray-500">毫秒级 OCI Go SDK 驱动 · 智能错误分类 · 指数退避 · 同名幂等防重开机</p>
      </div>
      <div class="flex items-center space-x-3">
        <n-button type="info" secondary @click="handleCreateDefaultVCN" :loading="vcnCreating">
          🌐 一键配置推荐全通 VCN
        </n-button>
      </div>
    </div>

    <!-- 1. Free Tier Quick Presets Cards -->
    <div class="space-y-2">
      <span class="text-xs font-bold text-gray-700 uppercase tracking-wider">🌟 官方免费额度最佳实践预设 (点击一键填入)</span>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3">
        <div
          v-for="preset in presets"
          :key="preset.id"
          class="bg-white p-3.5 rounded-xl border border-gray-200 hover:border-red-500 hover:shadow-md transition-all cursor-pointer space-y-1.5 group"
          @click="applyPreset(preset)"
        >
          <div class="text-xs font-bold text-gray-900 group-hover:text-red-600 truncate">{{ preset.name }}</div>
          <div class="text-[11px] text-gray-500">
            {{ preset.shape.includes('A1') ? 'ARM Ampere' : 'AMD Micro' }} · {{ preset.ocpu }}C / {{ preset.memory_in_gbs }}G
          </div>
          <div class="flex justify-between items-center text-[10px] text-gray-400">
            <span>引导卷: {{ preset.boot_volume_size_in_gbs }}GB</span>
            <span class="text-red-500 font-semibold">120 VPU</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 2. Main Form Grid -->
    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
      <!-- Left 2 Cols: Form Options -->
      <div class="lg:col-span-2 bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-5">
        <h3 class="text-sm font-bold text-gray-900 border-b border-gray-100 pb-2">基础规格与网络配置</h3>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <n-form-item label="实例名称 (同名防重机制生效)">
            <n-input v-model:value="form.instance_name" placeholder="如 oci-arm-vm1" />
          </n-form-item>

          <n-form-item label="架构与 Shape">
            <n-select
              v-model:value="form.shape"
              :options="shapeOptions"
              @update:value="onShapeChange"
            />
          </n-form-item>
        </div>

        <!-- CPU & RAM (Flex slider for ARM) -->
        <div v-if="form.shape.includes('A1')" class="grid grid-cols-1 sm:grid-cols-2 gap-4 bg-gray-50 p-4 rounded-xl border border-gray-100">
          <div>
            <div class="flex justify-between text-xs font-medium text-gray-700 mb-1">
              <span>OCPU 核心数</span>
              <span class="font-bold text-red-600">{{ form.ocpu }} 核</span>
            </div>
            <n-slider v-model:value="form.ocpu" :min="1" :max="4" :step="1" />
          </div>
          <div>
            <div class="flex justify-between text-xs font-medium text-gray-700 mb-1">
              <span>内存容量 (GB)</span>
              <span class="font-bold text-red-600">{{ form.memory_in_gbs }} GB</span>
            </div>
            <n-slider v-model:value="form.memory_in_gbs" :min="1" :max="24" :step="1" />
          </div>
        </div>

        <!-- Dynamic Official Ubuntu Image Selector -->
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <n-form-item label="官方 Ubuntu 正式版镜像 (自动识别架构)">
            <n-select
              v-model:value="form.image_ocid"
              :options="imageOptions"
              :loading="loadingImages"
              placeholder="正在动态探测最新两代正式版..."
            />
          </n-form-item>

          <n-form-item label="可用区 (AD) 轮询选择">
            <n-select
              v-model:value="form.ad_list"
              multiple
              :options="adOptions"
              placeholder="可多选可用区自动轮询"
            />
          </n-form-item>
        </div>

        <!-- Storage & VPU Performance -->
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <n-form-item label="引导卷大小 (GB，最小 47，受 200GB 免费限额约束)">
            <n-input-number v-model:value="form.boot_volume_size_in_gbs" :min="47" :max="200" class="w-full" />
          </n-form-item>

          <n-form-item label="引导卷性能 (VPU - 默认系统顶格 120 VPU 超高性能)">
            <n-select v-model:value="form.boot_volume_vpu" :options="vpuOptions" />
          </n-form-item>
        </div>

        <!-- Network VCN and Subnet -->
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <n-form-item label="VCN 虚拟网络">
            <n-select v-model:value="selectedVCN" :options="vcnOptions" @update:value="onVCNChange" placeholder="选择 VCN" />
          </n-form-item>

          <n-form-item label="公网子网 (Subnet)">
            <n-select v-model:value="form.subnet_ocid" :options="subnetOptions" placeholder="选择关联子网" />
          </n-form-item>
        </div>

        <!-- Dual Login Mode Selection -->
        <div class="space-y-3 bg-gray-50 p-4 rounded-xl border border-gray-100">
          <span class="text-xs font-bold text-gray-800">登录方式选择 (严格双选)</span>
          <n-radio-group v-model:value="form.login_mode" name="loginModeGroup">
            <n-space>
              <n-radio value="root_key">🔑 root + SSH 密钥登录</n-radio>
              <n-radio value="root_password">🔐 root + 20位随机密码 (自动保存在实例云端标签)</n-radio>
            </n-space>
          </n-radio-group>

          <!-- Key Mode -->
          <div v-if="form.login_mode === 'root_key'" class="pt-2">
            <n-input
              v-model:value="form.ssh_authorized_keys"
              type="textarea"
              placeholder="在此粘贴您的 SSH 公钥 (ssh-rsa / ssh-ed25519 ...)"
              rows="3"
            />
          </div>

          <!-- Random Password Mode -->
          <div v-else class="space-y-2 pt-2">
            <div class="flex items-center space-x-2">
              <n-input v-model:value="form.root_password" class="font-mono text-sm" placeholder="20位高熵随机密码" />
              <n-button secondary @click="generateRandomPassword">🔄 重新生成</n-button>
              <n-button @click="copyPassword">📋 复制</n-button>
            </div>
            <p class="text-[11px] text-gray-500">
              💡 开机后该密码将自动写入 OCI 实例的云端自由标签 <code>freeform_tags: {"root_password": "..."}</code>，可在实例管理随时查看和在线编辑。
            </p>
          </div>
        </div>

        <!-- Toggles: Public IPv4 and IPv6 -->
        <div class="flex flex-wrap gap-6 pt-1">
          <n-checkbox v-model:checked="form.assign_public_ip">
            分配公网 IPv4
          </n-checkbox>
          <n-checkbox v-model:checked="form.enable_ipv6">
            开通 IPv6 并自动放通防火墙规则
          </n-checkbox>
        </div>

        <!-- Submit Launch Task Button -->
        <div class="pt-4 border-t border-gray-100 flex items-center justify-between">
          <div class="text-xs text-gray-500">
            重试间隔: <b>{{ form.retry_interval_secs }}s</b> · 包含防限流随机 Jitter
          </div>
          <n-button type="primary" size="large" :loading="launching" @click="handleCreateTask">
            🚀 启动后台抢机重试任务
          </n-button>
        </div>
      </div>

      <!-- Right 1 Col: Live WebSocket Output Console -->
      <div class="bg-gray-900 text-gray-100 p-5 rounded-2xl border border-gray-800 shadow-sm flex flex-col h-[650px]">
        <div class="flex justify-between items-center border-b border-gray-800 pb-3 mb-3">
          <div class="flex items-center space-x-2">
            <span class="w-2.5 h-2.5 rounded-full" :class="wsConnected ? 'bg-emerald-500 animate-pulse' : 'bg-gray-500'"></span>
            <span class="text-xs font-mono font-bold">实时抢机终端日志 (WebSocket)</span>
          </div>
          <n-button size="tiny" quaternary text-color="#94a3b8" @click="logMessages = []">清空</n-button>
        </div>

        <!-- Terminal stream content -->
        <div ref="terminalBody" class="flex-1 overflow-y-auto space-y-2 font-mono text-xs pr-1">
          <div v-if="logMessages.length === 0" class="text-gray-600 text-center py-20">
            等待任务启动...<br />启动后尝试记录、耗时与错误细分将毫秒级流式打印在此
          </div>
          <div
            v-for="(msg, idx) in logMessages"
            :key="idx"
            class="p-2 rounded bg-gray-800/60 border border-gray-700/40 space-y-1"
          >
            <div class="flex justify-between text-[10px] text-gray-400">
              <span>[{{ msg.timestamp }}] 第 {{ msg.attempt_num }} 轮</span>
              <span>耗时: {{ msg.duration_ms }}ms</span>
            </div>
            <div
              :class="msg.status === 'SUCCESS' ? 'text-emerald-400 font-bold' : (msg.status === 'FATAL' ? 'text-red-400 font-bold' : 'text-amber-300')"
            >
              {{ msg.message }}
            </div>
          </div>
        </div>

        <div class="border-t border-gray-800 pt-3 mt-2 text-[10px] text-gray-500 flex justify-between">
          <span>状态: {{ wsConnected ? '已连接' : '未连接' }}</span>
          <span>自动识别同名实例防重开机</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { useMessage } from 'naive-ui'

const profileStore = useProfileStore()
const message = useMessage()

const launching = ref(false)
const vcnCreating = ref(false)
const loadingImages = ref(false)
const presets = ref<any[]>([])
const selectedVCN = ref<string | null>(null)
const vcnOptions = ref<any[]>([])
const subnetOptions = ref<any[]>([])
const imageOptions = ref<any[]>([])
const adOptions = ref<any[]>([])

const wsConnected = ref(false)
const logMessages = ref<any[]>([])
const terminalBody = ref<HTMLElement | null>(null)
let socket: WebSocket | null = null

const form = ref({
  instance_name: 'oci-free-vm',
  shape: 'VM.Standard.A1.Flex',
  ocpu: 2,
  memory_in_gbs: 12,
  boot_volume_size_in_gbs: 50,
  boot_volume_vpu: 120, // default maximum 120 VPU
  region: '',
  ad_list: [] as string[],
  image_ocid: '',
  subnet_ocid: '',
  login_mode: 'root_key',
  ssh_authorized_keys: '',
  root_password: '',
  assign_public_ip: true,
  enable_ipv6: true,
  retry_interval_secs: 60,
  max_retries: 0,
})

const shapeOptions = [
  { label: 'ARM Ampere Flex (可自定义核数与内存)', value: 'VM.Standard.A1.Flex' },
  { label: 'AMD 微型机 (VM.Standard.E2.1.Micro)', value: 'VM.Standard.E2.1.Micro' },
]

const vpuOptions = [
  { label: '120 VPU - 超高性能 (Ultra High Performance，最高50,000 IOPS - 推荐)', value: 120 },
  { label: '60 VPU - 极速模式 (Very High Performance)', value: 60 },
  { label: '20 VPU - 高性能 (Higher Performance)', value: 20 },
  { label: '10 VPU - 均衡模式 (Balanced，OCI 引导卷法定最低档位)', value: 10 },
]

const currentProfile = computed(() => {
  return profileStore.profiles.find(p => p.id === profileStore.activeProfileId)
})

const generateRandomPassword = () => {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789!@#$%^&*()_+'
  let pass = ''
  for (let i = 0; i < 20; i++) {
    pass += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  form.value.root_password = pass
}

const copyPassword = () => {
  navigator.clipboard.writeText(form.value.root_password)
  message.success('密码已复制到剪贴板')
}

const applyPreset = (preset: any) => {
  form.value.shape = preset.shape
  form.value.ocpu = preset.ocpu
  form.value.memory_in_gbs = preset.memory_in_gbs
  form.value.boot_volume_size_in_gbs = preset.boot_volume_size_in_gbs
  form.value.boot_volume_vpu = preset.boot_volume_vpu || 120
  form.value.enable_ipv6 = preset.enable_ipv6
  message.success(`已载入预设: ${preset.name}`)
  loadImages()
}

const onShapeChange = () => {
  if (form.value.shape.includes('Micro')) {
    form.value.ocpu = 1
    form.value.memory_in_gbs = 1
  } else {
    form.value.ocpu = 2
    form.value.memory_in_gbs = 12
  }
  loadImages()
}

const loadImages = async () => {
  if (!profileStore.activeProfileId) return
  loadingImages.value = true
  try {
    const res: any = await api.get(`/tasks/images?profile_id=${profileStore.activeProfileId}&shape=${form.value.shape}&region=${form.value.region || currentProfile.value?.region}`)
    imageOptions.value = (res.images || []).map((img: any) => ({
      label: `Ubuntu ${img.version} LTS (${img.architecture}) - ${img.display_name}`,
      value: img.ocid,
    }))
    if (imageOptions.value.length > 0 && !form.value.image_ocid) {
      form.value.image_ocid = imageOptions.value[0].value
    }
  } catch (e: any) {
    message.error('拉取 Ubuntu 镜像失败: ' + e.message)
  } finally {
    loadingImages.value = false
  }
}

const loadNetworks = async () => {
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
  } catch (e) {}
}

const onVCNChange = async () => {
  if (!selectedVCN.value || !profileStore.activeProfileId) return
  try {
    const res: any = await api.get(`/network/subnets?profile_id=${profileStore.activeProfileId}&vcn_id=${selectedVCN.value}`)
    subnetOptions.value = (res.subnets || []).map((s: any) => ({
      label: `${s.display_name} (${s.cidr_block})`,
      value: s.ocid,
    }))
    if (subnetOptions.value.length > 0) {
      form.value.subnet_ocid = subnetOptions.value[0].value
    }
  } catch (e) {}
}

const handleCreateDefaultVCN = async () => {
  if (!profileStore.activeProfileId || !currentProfile.value) return
  vcnCreating.value = true
  try {
    const res: any = await api.post('/network/create-default-vcn', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value.region,
    })
    message.success(res.message)
    await loadNetworks()
    if (res.subnet_id) {
      form.value.subnet_ocid = res.subnet_id
    }
  } catch (e: any) {
    message.error(e.message)
  } finally {
    vcnCreating.value = false
  }
}

const connectWebSocket = (taskID: string) => {
  if (socket) socket.close()
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${protocol}//${window.location.host}/ws/logs/${taskID}`
  socket = new WebSocket(url)

  socket.onopen = () => {
    wsConnected.value = true
  }
  socket.onmessage = (event) => {
    try {
      const parsed = JSON.parse(event.data)
      logMessages.value.unshift(parsed)
      nextTick(() => {
        if (terminalBody.value) terminalBody.value.scrollTop = 0
      })
    } catch (e) {}
  }
  socket.onclose = () => {
    wsConnected.value = false
  }
}

const handleCreateTask = async () => {
  if (!profileStore.activeProfileId) {
    message.error('请选择一个 OCI 账号')
    return
  }
  if (!form.value.image_ocid || !form.value.subnet_ocid) {
    message.warning('请选择镜像和子网')
    return
  }

  launching.value = true
  try {
    const res: any = await api.post('/tasks/create', {
      profile_id: profileStore.activeProfileId,
      instance_name: form.value.instance_name,
      shape: form.value.shape,
      ocpu: form.value.ocpu,
      memory_in_gbs: form.value.memory_in_gbs,
      boot_volume_size_in_gbs: form.value.boot_volume_size_in_gbs,
      boot_volume_vpu: form.value.boot_volume_vpu,
      region: currentProfile.value?.region,
      ad_list: form.value.ad_list,
      image_ocid: form.value.image_ocid,
      subnet_ocid: form.value.subnet_ocid,
      login_mode: form.value.login_mode,
      ssh_authorized_keys: form.value.ssh_authorized_keys,
      root_password: form.value.root_password,
      assign_public_ip: form.value.assign_public_ip,
      enable_ipv6: form.value.enable_ipv6,
      retry_interval_secs: form.value.retry_interval_secs,
      max_retries: form.value.max_retries,
    })

    message.success(res.message)
    if (res.task_id) {
      connectWebSocket(res.task_id)
    }
  } catch (e: any) {
    message.error(e.message)
  } finally {
    launching.value = false
  }
}

const loadPresets = async () => {
  try {
    const res: any = await api.get('/tasks/presets')
    presets.value = res.presets || []
  } catch (e) {}
}

watch(() => profileStore.activeProfileId, () => {
  if (currentProfile.value) {
    form.value.region = currentProfile.value.region
  }
  loadImages()
  loadNetworks()
})

onMounted(async () => {
  generateRandomPassword()
  await loadPresets()
  if (currentProfile.value) {
    form.value.region = currentProfile.value.region
  }
  await loadImages()
  await loadNetworks()
})
</script>
