<template>
  <div>
    <PageHeader title="创建实例" description="填写规格后立即创建。容量不足时可加入排队：ARM A1 每 3–5 分钟查询一次官方容量报告，报告有容量时才发起创建；AMD Micro 按相同间隔直接尝试。排队最长持续 7 天。" />

    <!-- No VCN yet -->
    <div v-if="!loadingNets && netsLoaded && vcnOptions.length === 0" class="notice notice-warn mb-6 items-center">
      <n-icon size="18" class="shrink-0"><WarningOutline /></n-icon>
      <div class="flex flex-1 flex-wrap items-center justify-between gap-3">
        <span>该账号在当前区域尚无虚拟云网络（VCN），创建实例前需先创建。推荐网络包含公共子网、互联网网关与 IPv6。</span>
        <n-button type="primary" size="small" :loading="vcnCreating" @click="handleCreateDefaultVCN">
          <template #icon><n-icon><GlobeOutline /></n-icon></template>
          一键创建推荐 VCN
        </n-button>
      </div>
    </div>

    <!-- Queued retries: only shown while something is retrying -->
    <div v-if="activeTasks.length" class="mb-6 space-y-2">
      <div v-for="t in activeTasks" :key="t.id" class="notice notice-info items-center">
        <span class="relative flex h-2.5 w-2.5 shrink-0">
          <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-info opacity-50"></span>
          <span class="relative inline-flex h-2.5 w-2.5 rounded-full bg-info"></span>
        </span>
        <div class="flex flex-1 flex-wrap items-center justify-between gap-3">
          <span>
            <b class="mono">{{ t.instance_name }}</b>
            {{ taskStripLabel(t) }}
            <span v-if="t.status === 'running'" class="mono">· 已检查 {{ t.current_retries }} 次</span>
            <span v-if="t.status === 'running' && t.last_message" class="ml-1 text-ink-2">· {{ t.last_message }}</span>
          </span>
          <n-button size="small" secondary :type="t.status === 'running' ? 'warning' : 'default'" :loading="taskActing === t.id" @click="stopTask(t)">
            {{ t.status === 'running' ? '停止排队' : '清除' }}
          </n-button>
        </div>
      </div>
    </div>

    <!-- ===== Presets ===== -->
    <section v-if="presets.length" class="mb-6">
      <div class="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h2 class="section-title">免费额度预设</h2>
        <span class="caption">
          当前账号：<b class="text-ink">{{ accountType === 'payg' ? '升级号' : '免费号' }}</b>，ARM 免费额度
          <b class="mono text-ink">{{ allowance.ocpu }} OCPU / {{ allowance.memory_gb }} GB</b>
        </span>
      </div>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <button
          v-for="preset in presets"
          :key="preset.id"
          type="button"
          class="card p-3.5 text-left transition-all hover:border-line-strong hover:shadow-card"
          :class="selectedPresetId === preset.id ? '!border-brand ring-2 ring-brand/20' : ''"
          @click="applyPreset(preset)"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="truncate text-[13px] font-semibold text-ink">{{ preset.name }}</span>
            <span v-if="preset.is_max" class="pill pill-info">满配</span>
          </div>
          <div class="mono mt-1 text-xs text-ink-2">{{ preset.shape.includes('A1') ? 'ARM Ampere A1' : 'AMD E2.1 Micro' }}</div>
          <div class="mono mt-0.5 text-xs text-ink-3">{{ preset.ocpu }} OCPU · {{ preset.memory_in_gbs }} GB · {{ preset.boot_volume_size_in_gbs }} GB 引导卷<span v-if="preset.enable_ipv6"> · IPv6</span></div>
        </button>
      </div>
    </section>

    <!-- ===== Form ===== -->
    <section class="card card-pad">
      <n-form label-placement="top" :show-feedback="false" @submit.prevent="handleCreateTask">
        <div class="space-y-7">
          <!-- 规格 -->
          <fieldset class="space-y-4">
            <legend class="mb-3 text-xs font-semibold uppercase tracking-wider text-ink-3">规格</legend>
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <n-form-item label="实例名称">
                <n-input v-model:value="form.instance_name" placeholder="留空则自动生成" :input-props="{ spellcheck: 'false' }" class="mono">
                  <template #suffix>
                    <button type="button" class="txt-btn-muted" title="重新生成名称" @click="form.instance_name = randomInstanceName()">重新生成</button>
                  </template>
                </n-input>
              </n-form-item>
              <n-form-item label="Shape">
                <n-select v-model:value="form.shape" :options="shapeOptions" @update:value="onShapeChange" />
              </n-form-item>
            </div>
            <p class="caption -mt-2">名称按控制台默认样式生成；同名实例已存在时不会重复创建。</p>

            <div v-if="isA1" class="grid grid-cols-1 gap-5 rounded-lg border border-line bg-surface-2 p-4 sm:grid-cols-2">
              <div>
                <div class="mb-1.5 flex items-center justify-between">
                  <span class="label mb-0">OCPU</span>
                  <span class="mono text-sm font-semibold text-ink">{{ form.ocpu }} 核</span>
                </div>
                <n-slider v-model:value="form.ocpu" :min="1" :max="allowance.ocpu" :step="1" :marks="ocpuMarks" />
              </div>
              <div>
                <div class="mb-1.5 flex items-center justify-between">
                  <span class="label mb-0">内存</span>
                  <span class="mono text-sm font-semibold text-ink">{{ form.memory_in_gbs }} GB</span>
                </div>
                <n-slider v-model:value="form.memory_in_gbs" :min="1" :max="allowance.memory_gb" :step="1" :marks="memMarks" />
              </div>
              <p class="caption sm:col-span-2">
                {{ accountType === 'payg' ? '升级号' : '免费号' }}最多 {{ allowance.ocpu }} OCPU / {{ allowance.memory_gb }} GB，每 OCPU 建议搭配 6 GB 内存。
              </p>
            </div>
            <p v-else class="caption">VM.Standard.E2.1.Micro 固定 1 OCPU / 1 GB，免费额度最多 2 台。</p>
          </fieldset>

          <!-- 镜像与可用区 -->
          <fieldset class="space-y-4">
            <legend class="mb-3 text-xs font-semibold uppercase tracking-wider text-ink-3">镜像与可用区</legend>
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <n-form-item label="Ubuntu 镜像">
                <n-select v-model:value="form.image_ocid" :options="imageOptions" :loading="loadingImages" placeholder="正在读取最新两代 LTS 镜像…" />
              </n-form-item>
              <n-form-item label="可用区">
                <n-select v-model:value="form.ad_list" multiple :options="adOptions" :loading="loadingADs" placeholder="留空则依次尝试全部可用区" max-tag-count="responsive" />
              </n-form-item>
            </div>
            <p class="caption -mt-2">镜像按 Shape 架构自动筛选：A1 使用 aarch64，E2.1.Micro 使用 x86_64。</p>
          </fieldset>

          <!-- 引导卷 -->
          <fieldset class="space-y-4">
            <legend class="mb-3 text-xs font-semibold uppercase tracking-wider text-ink-3">引导卷</legend>
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <n-form-item label="容量（GB）">
                <n-input-number v-model:value="form.boot_volume_size_in_gbs" :min="50" :max="200" :step="10" class="w-full" />
              </n-form-item>
              <n-form-item label="性能档位">
                <n-select v-model:value="form.boot_volume_vpu" :options="vpuOptions" />
              </n-form-item>
            </div>
            <p class="caption -mt-2">引导卷与块存储合计免费 200 GB；高于 10 VPU 的档位在升级号上按 VPU 计费。</p>
          </fieldset>

          <!-- 网络 -->
          <fieldset class="space-y-4">
            <legend class="mb-3 text-xs font-semibold uppercase tracking-wider text-ink-3">网络</legend>
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <n-form-item label="VCN">
                <n-select v-model:value="selectedVCN" :options="vcnOptions" :loading="loadingNets" placeholder="选择 VCN" @update:value="onVCNChange" />
              </n-form-item>
              <n-form-item label="子网">
                <n-select v-model:value="form.subnet_ocid" :options="subnetOptions" placeholder="选择子网" :disabled="!selectedVCN" />
              </n-form-item>
            </div>
            <div class="flex flex-wrap gap-x-6 gap-y-2">
              <n-checkbox v-model:checked="form.assign_public_ip">分配公网 IPv4</n-checkbox>
              <n-checkbox v-model:checked="form.enable_ipv6">分配 IPv6 地址</n-checkbox>
              <n-checkbox v-model:checked="form.open_all_ports">创建后启用专属防火墙并放通全部端口（检测到 IPv6 地址时一并放通）</n-checkbox>
            </div>
          </fieldset>

          <!-- 登录方式 -->
          <fieldset class="space-y-3">
            <legend class="mb-3 text-xs font-semibold uppercase tracking-wider text-ink-3">登录方式</legend>
            <n-radio-group v-model:value="form.login_mode" name="loginMode">
              <n-space :size="20">
                <n-radio value="root_key">root + SSH 密钥</n-radio>
                <n-radio value="root_password">root + 随机密码</n-radio>
              </n-space>
            </n-radio-group>
            <div v-if="form.login_mode === 'root_key'" class="space-y-2">
              <div v-if="sshKeys.length" class="flex flex-wrap items-center gap-2">
                <n-select
                  v-model:value="selectedKeyId"
                  :options="sshKeyOptions"
                  placeholder="选择已保存的公钥"
                  clearable
                  size="small"
                  class="w-full sm:w-[340px]"
                  @update:value="applySavedKey"
                />
                <n-button v-if="selectedKeyId && selectedKeyId !== currentProfile?.default_ssh_key_id" size="small" secondary @click="setAccountDefaultKey">设为该账号默认</n-button>
                <span v-else-if="selectedKeyId" class="caption">该账号默认</span>
                <router-link to="/settings" class="txt-btn-muted">管理公钥</router-link>
              </div>
              <n-input
                v-model:value="form.ssh_authorized_keys"
                type="textarea"
                class="mono"
                placeholder="粘贴 SSH 公钥（ssh-ed25519 … 或 ssh-rsa …），或从文件导入"
                :rows="3"
                :input-props="{ spellcheck: 'false' }"
              />
              <div class="flex flex-wrap items-center gap-3">
                <n-button secondary size="small" @click="keyFileInput?.click()">
                  <template #icon><n-icon><CloudUploadOutline /></n-icon></template>
                  从文件导入公钥
                </n-button>
                <span v-if="keyFileName" class="caption">已导入 {{ keyFileName }}</span>
                <input ref="keyFileInput" type="file" accept=".pub,.txt,.pem,text/plain" class="sr-only" @change="onKeyFileSelected" />
                <button v-if="form.ssh_authorized_keys.trim() && !currentKeySaved" type="button" class="txt-btn" @click="openSaveKey">保存到公钥库</button>
                <span v-else-if="form.ssh_authorized_keys.trim() && currentKeySaved" class="caption">已在公钥库中</span>
              </div>
              <n-modal v-model:show="showSaveKey" preset="card" title="保存到公钥库" style="max-width: 420px" :bordered="false">
                <div class="space-y-4">
                  <p class="caption">保存后可在「设置 → SSH 公钥」中管理，创建实例时可直接选择。</p>
                  <n-form-item label="名称" label-placement="top" :show-feedback="false">
                    <n-input v-model:value="saveKeyName" placeholder="例如：MacBook、工作电脑" maxlength="64" @keyup.enter="submitSaveKey" />
                  </n-form-item>
                  <div class="flex justify-end gap-2 pt-1">
                    <n-button @click="showSaveKey = false">取消</n-button>
                    <n-button type="primary" :loading="savingKey" @click="submitSaveKey">保存</n-button>
                  </div>
                </div>
              </n-modal>
            </div>
            <div v-else class="space-y-2">
              <div class="flex flex-col gap-2 sm:flex-row">
                <n-input v-model:value="form.root_password" class="mono flex-1" placeholder="20 位随机密码" :input-props="{ spellcheck: 'false', autocomplete: 'off' }" />
                <div class="flex gap-2">
                  <n-button secondary @click="generateRandomPassword">
                    <template #icon><n-icon><RefreshOutline /></n-icon></template>
                    重新生成
                  </n-button>
                  <n-button secondary @click="copyPassword">
                    <template #icon><n-icon><CopyOutline /></n-icon></template>
                    复制
                  </n-button>
                </div>
              </div>
              <p class="caption">创建后密码会写入实例的云端标签 <code class="mono">root_password</code>，可在「实例」页查看与修改。</p>
            </div>
          </fieldset>

          <div class="flex flex-col gap-3 border-t border-line pt-5 sm:flex-row sm:items-center sm:justify-between">
            <span class="caption">点击后先显示参数确认，确认后立即向 OCI 发起创建。同一时间仅允许操作一个账号。</span>
            <n-button type="primary" size="large" attr-type="submit" :loading="launching" :disabled="!currentProfile || vcnOptions.length === 0">
              <template #icon><n-icon><RocketOutline /></n-icon></template>
              {{ launching ? '正在创建…' : '创建实例' }}
            </n-button>
          </div>
        </div>
      </n-form>
    </section>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, h } from 'vue'
import {
  NButton,
  NIcon,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NInputNumber,
  NSelect,
  NSlider,
  NCheckbox,
  NRadioGroup,
  NRadio,
  NSpace,
  useMessage,
  useDialog,
} from 'naive-ui'
import { GlobeOutline, RefreshOutline, CopyOutline, RocketOutline, CloudUploadOutline, WarningOutline } from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { regionLabel } from '@/lib/regions'
import { defaultName } from '@/lib/naming'
import PageHeader from '@/components/PageHeader.vue'

const profileStore = useProfileStore()
const message = useMessage()
const dialog = useDialog()

const launching = ref(false)
const vcnCreating = ref(false)
const loadingImages = ref(false)
const loadingNets = ref(false)
const netsLoaded = ref(false)
const loadingADs = ref(false)
const presets = ref<any[]>([])
const accountType = ref<'free' | 'payg'>('free')
const allowance = ref({ ocpu: 2, memory_gb: 12 })
const selectedPresetId = ref<number | null>(null)
const selectedVCN = ref<string | null>(null)
const vcnOptions = ref<any[]>([])
const subnetOptions = ref<any[]>([])
const imageOptions = ref<any[]>([])
const adOptions = ref<any[]>([])
const keyFileInput = ref<HTMLInputElement | null>(null)
const keyFileName = ref('')

const tasks = ref<any[]>([])
const loadingTasks = ref(false)
const taskActing = ref('')
let pollTimer: number | null = null


// Project-style random names: adjective-noun-NN
const randomInstanceName = () => defaultName('instance')

const form = ref({
  instance_name: randomInstanceName(),
  shape: 'VM.Standard.A1.Flex',
  ocpu: 2,
  memory_in_gbs: 12,
  boot_volume_size_in_gbs: 50,
  boot_volume_vpu: 10,
  region: '',
  ad_list: [] as string[],
  image_ocid: '',
  subnet_ocid: '',
  login_mode: 'root_key',
  ssh_authorized_keys: '',
  root_password: '',
  assign_public_ip: true,
  enable_ipv6: true,
  open_all_ports: true,
})

// ---------- saved SSH public keys ----------
const sshKeys = ref<any[]>([])
const selectedKeyId = ref<number | null>(null)
const sshKeyOptions = computed(() =>
  sshKeys.value.map((k) => ({ label: `${k.name}${k.is_default ? '（默认）' : ''} · ${k.key_type}`, value: k.id })),
)
const currentKeySaved = computed(() => sshKeys.value.some((k) => (k.public_key || '').trim() === form.value.ssh_authorized_keys.trim()))

// The default key is chosen per account. When the account changes, a key that came from the
// library is swapped for the new account's default (or cleared), never carried over silently.
const loadSSHKeys = async () => {
  try {
    const res: any = await api.get('/ssh-keys')
    sshKeys.value = res.keys || []
    const current = form.value.ssh_authorized_keys.trim()
    const fromLibrary = !!current && sshKeys.value.some((k) => (k.public_key || '').trim() === current)
    const def = sshKeys.value.find((k) => k.id === currentProfile.value?.default_ssh_key_id)
    if (def && (!current || fromLibrary)) {
      form.value.ssh_authorized_keys = def.public_key
      selectedKeyId.value = def.id
    } else if (!def && fromLibrary) {
      form.value.ssh_authorized_keys = ''
      selectedKeyId.value = null
    }
  } catch {
    /* the key library is optional */
  }
}

const applySavedKey = (id: number | null) => {
  const k = sshKeys.value.find((x) => x.id === id)
  if (!k) return
  form.value.ssh_authorized_keys = k.public_key
  keyFileName.value = ''
  const others = (k.default_for || []).filter((n: string) => n !== currentProfile.value?.name)
  if (others.length) message.warning(`该公钥同时为 ${others.join('、')} 的默认公钥；多个账号共用同一公钥存在被关联的风险`)
}

const setAccountDefaultKey = async () => {
  if (!selectedKeyId.value || !profileStore.activeProfileId) return
  try {
    const res: any = await api.post(`/ssh-keys/default/${selectedKeyId.value}?profile_id=${profileStore.activeProfileId}`)
    if (res.shared_with?.length) message.warning(res.message)
    else message.success(res.message || '已设为该账号默认公钥')
    await Promise.all([profileStore.fetchProfiles(), loadSSHKeys()])
  } catch (e: any) {
    message.error(e.message)
  }
}

// Editing the textarea by hand detaches it from the selected saved key.
watch(
  () => form.value.ssh_authorized_keys,
  (v) => {
    const k = sshKeys.value.find((x) => x.id === selectedKeyId.value)
    if (k && (k.public_key || '').trim() !== v.trim()) selectedKeyId.value = null
  },
)

const showSaveKey = ref(false)
const saveKeyName = ref('')
const savingKey = ref(false)
const openSaveKey = () => {
  saveKeyName.value = keyFileName.value.replace(/\.pub$/i, '')
  showSaveKey.value = true
}
const submitSaveKey = async () => {
  if (!saveKeyName.value.trim()) {
    message.warning('请填写公钥名称')
    return
  }
  savingKey.value = true
  try {
    const res: any = await api.post('/ssh-keys', { name: saveKeyName.value.trim(), public_key: form.value.ssh_authorized_keys.trim() })
    message.success('公钥已保存')
    showSaveKey.value = false
    await loadSSHKeys()
    if (res.key?.id) selectedKeyId.value = res.key.id
  } catch (e: any) {
    message.error(e.message)
  } finally {
    savingKey.value = false
  }
}

const shapeOptions = [
  { label: 'ARM Ampere A1 Flex（可调核数与内存）', value: 'VM.Standard.A1.Flex' },
  { label: 'AMD E2.1 Micro（1 OCPU / 1 GB）', value: 'VM.Standard.E2.1.Micro' },
]

const vpuOptions = [
  { label: '10 VPU · 均衡（控制台默认）', value: 10 },
  { label: '20 VPU · 高性能', value: 20 },
  { label: '60 VPU · 超高性能', value: 60 },
  { label: '120 VPU · 超高性能', value: 120 },
]

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))
const isA1 = computed(() => form.value.shape.includes('A1'))
// creating: synchronous LaunchInstance in flight · running: queued retries. A task is "success"
// as soon as LaunchInstance returns an instance OCID.
const isActiveTask = (t: any) => t.status === 'running' || t.status === 'creating'
const activeTasks = computed(() => tasks.value.filter(isActiveTask))
const taskStripLabel = (t: any) => (t.status === 'creating' ? '正在创建…' : '排队中，等待容量')

// The active task (queued retries, or a create in flight elsewhere) whose final outcome gets
// announced on this page. Cleared once announced or when the user stops/clears it.
const watchedTaskId = ref('')
const watchedPrevStatus = ref('')
const announceOutcome = (t: any) => {
  if (t.status === 'success') {
    dialog.success({
      title: '实例创建成功',
      content: `${t.instance_name}：${t.last_message || '已在 OCI 创建成功'}`,
      positiveText: '确定',
    })
  } else if (t.status === 'failed') {
    dialog.error({ title: '创建失败', content: t.last_message || '未知错误', positiveText: '确定' })
  } else if (t.status === 'stopped' && watchedPrevStatus.value !== 'running') {
    askAutoRetry({ task_id: t.id, reason: t.last_message || '实例创建失败', attempts: t.current_retries || 1 })
  } else if (t.status === 'stopped') {
    dialog.info({ title: '排队已结束', content: `${t.instance_name}：${t.last_message || '已停止'}`, positiveText: '确定' })
  } else {
    return
  }
  watchedTaskId.value = ''
  watchedPrevStatus.value = ''
}
const trackWatchedTask = () => {
  if (!watchedTaskId.value) {
    const pending = tasks.value.find(isActiveTask)
    if (pending) {
      watchedTaskId.value = pending.id
      watchedPrevStatus.value = pending.status
    }
    return
  }
  const w = tasks.value.find((t) => t.id === watchedTaskId.value)
  if (!w) {
    watchedTaskId.value = ''
    watchedPrevStatus.value = ''
  } else if (isActiveTask(w)) {
    watchedPrevStatus.value = w.status
  } else {
    announceOutcome(w)
  }
}

const ocpuMarks = computed(() => {
  const m: Record<number, string> = {}
  for (let i = 1; i <= allowance.value.ocpu; i++) m[i] = String(i)
  return m
})
const memMarks = computed(() => {
  const m: Record<number, string> = {}
  for (let i = 6; i <= allowance.value.memory_gb; i += 6) m[i] = String(i)
  return m
})

const shortAD = (ad?: string) => (ad ? ad.replace(/^[^:]+:/, '') : '')


const generateRandomPassword = () => {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789!@#%^*_+-='
  const buf = new Uint32Array(20)
  crypto.getRandomValues(buf)
  form.value.root_password = Array.from(buf, (n) => chars[n % chars.length]).join('')
}

const copyPassword = async () => {
  try {
    await navigator.clipboard.writeText(form.value.root_password)
    message.success('密码已复制')
  } catch {
    message.error('复制失败，请手动复制')
  }
}

const onKeyFileSelected = (event: Event) => {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = (e) => {
    const text = String(e.target?.result || '').trim()
    if (!/^(ssh-(rsa|ed25519|dss)|ecdsa-sha2-nistp\d+|sk-)/.test(text)) {
      message.warning('所选文件不是有效的 SSH 公钥（应以 ssh-ed25519 或 ssh-rsa 开头）。请选择 .pub 公钥文件，而非私钥。')
      target.value = ''
      return
    }
    form.value.ssh_authorized_keys = text
    keyFileName.value = file.name
    message.success(`已导入公钥 ${file.name}`)
    target.value = ''
  }
  reader.readAsText(file)
}

const clampToAllowance = () => {
  if (!isA1.value) return
  if (form.value.ocpu > allowance.value.ocpu) form.value.ocpu = allowance.value.ocpu
  if (form.value.memory_in_gbs > allowance.value.memory_gb) form.value.memory_in_gbs = allowance.value.memory_gb
}

const applyPreset = (preset: any) => {
  selectedPresetId.value = preset.id
  form.value.shape = preset.shape
  form.value.ocpu = preset.ocpu
  form.value.memory_in_gbs = preset.memory_in_gbs
  form.value.boot_volume_size_in_gbs = preset.boot_volume_size_in_gbs
  form.value.boot_volume_vpu = preset.boot_volume_vpu || 10
  form.value.enable_ipv6 = preset.enable_ipv6
  if (preset.login_mode) form.value.login_mode = preset.login_mode
  form.value.image_ocid = ''
  message.success(`已载入预设：${preset.name}`)
  loadImages()
}

const onShapeChange = () => {
  selectedPresetId.value = null
  if (form.value.shape.includes('Micro')) {
    form.value.ocpu = 1
    form.value.memory_in_gbs = 1
  } else {
    form.value.ocpu = Math.min(2, allowance.value.ocpu)
    form.value.memory_in_gbs = Math.min(12, allowance.value.memory_gb)
  }
  form.value.image_ocid = ''
  loadImages()
}

const loadPresets = async () => {
  try {
    const pid = profileStore.activeProfileId ? `?profile_id=${profileStore.activeProfileId}` : ''
    const res: any = await api.get(`/tasks/presets${pid}`)
    presets.value = res.presets || []
    accountType.value = res.account_type === 'payg' ? 'payg' : 'free'
    if (res.allowance?.ocpu) allowance.value = { ocpu: res.allowance.ocpu, memory_gb: res.allowance.memory_gb }
    clampToAllowance()
  } catch {
    presets.value = []
  }
}

const loadImages = async () => {
  if (!profileStore.activeProfileId) return
  loadingImages.value = true
  try {
    const region = form.value.region || currentProfile.value?.region || ''
    const res: any = await api.get(`/tasks/images?profile_id=${profileStore.activeProfileId}&shape=${encodeURIComponent(form.value.shape)}&region=${encodeURIComponent(region)}`)
    imageOptions.value = (res.images || []).map((img: any) => ({
      label: `Ubuntu ${img.version} LTS · ${img.architecture} · ${img.display_name}`,
      value: img.ocid,
    }))
    const stillValid = imageOptions.value.some((o) => o.value === form.value.image_ocid)
    if (!stillValid) form.value.image_ocid = imageOptions.value[0]?.value || ''
  } catch (e: any) {
    imageOptions.value = []
    message.error('读取 Ubuntu 镜像失败：' + e.message)
  } finally {
    loadingImages.value = false
  }
}

const loadADs = async () => {
  if (!profileStore.activeProfileId) return
  loadingADs.value = true
  try {
    const res: any = await api.get(`/network/ads?profile_id=${profileStore.activeProfileId}`)
    adOptions.value = (res.ads || []).map((a: any) => {
      const name = typeof a === 'string' ? a : a.name
      return { label: name, value: name }
    })
  } catch {
    adOptions.value = []
  } finally {
    loadingADs.value = false
  }
}

const loadNetworks = async () => {
  if (!profileStore.activeProfileId) return
  loadingNets.value = true
  try {
    const res: any = await api.get(`/network/vcns?profile_id=${profileStore.activeProfileId}`)
    vcnOptions.value = (res.vcns || []).map((v: any) => ({ label: `${v.display_name} (${v.cidr_block})${v.compartment && v.compartment !== 'root' ? ' · ' + v.compartment : ''}`, value: v.ocid }))
    if (vcnOptions.value.length > 0) {
      selectedVCN.value = vcnOptions.value[0].value
      await onVCNChange()
    } else {
      selectedVCN.value = null
      subnetOptions.value = []
      form.value.subnet_ocid = ''
    }
    netsLoaded.value = true
  } catch (e: any) {
    message.error('读取 VCN 失败：' + e.message)
  } finally {
    loadingNets.value = false
  }
}

const onVCNChange = async () => {
  form.value.subnet_ocid = ''
  subnetOptions.value = []
  if (!selectedVCN.value || !profileStore.activeProfileId) return
  try {
    const res: any = await api.get(`/network/subnets?profile_id=${profileStore.activeProfileId}&vcn_id=${selectedVCN.value}`)
    subnetOptions.value = (res.subnets || []).map((s: any) => ({ label: `${s.display_name} (${s.cidr_block})`, value: s.ocid }))
    if (subnetOptions.value.length > 0) form.value.subnet_ocid = subnetOptions.value[0].value
  } catch (e: any) {
    message.error('读取子网失败：' + e.message)
  }
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
    for (const w of res.warnings || []) message.warning(w, { duration: 8000 })
    await loadNetworks()
    if (res.subnet_id) form.value.subnet_ocid = res.subnet_id
  } catch (e: any) {
    message.error(e.message)
  } finally {
    vcnCreating.value = false
  }
}

// ---------- records ----------
const fetchTasks = async () => {
  if (!profileStore.activeProfileId) return
  loadingTasks.value = true
  try {
    const res: any = await api.get(`/tasks?profile_id=${profileStore.activeProfileId}`)
    tasks.value = res.tasks || []
    trackWatchedTask()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loadingTasks.value = false
  }
}

const stopTask = async (t: any) => {
  taskActing.value = t.id
  if (watchedTaskId.value === t.id) {
    watchedTaskId.value = ''
    watchedPrevStatus.value = ''
  }
  try {
    const res: any = await api.post(`/tasks/stop/${t.id}`)
    message.success(t.status === 'running' ? res.message || '已停止排队' : '已清除')
    await fetchTasks()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    taskActing.value = ''
  }
}

// ---------- create ----------
const askAutoRetry = (res: any) => {
  dialog.warning({
    title: '创建失败，是否自动重试？',
    content: `${res.reason || '容量不足'}。已尝试 ${res.attempts || 1} 个可用区。加入排队后，ARM A1 每 3–5 分钟查询一次官方容量报告，报告有容量时才发起创建；AMD Micro 按相同间隔直接尝试。直至创建成功、达到 7 天上限或手动停止。`,
    positiveText: '自动重试',
    negativeText: '不重试',
    onPositiveClick: async () => {
      try {
        const start: any = await api.post(`/tasks/start/${res.task_id}`)
        message.success(start.message || '已加入排队')
        await fetchTasks()
      } catch (e: any) {
        message.error(e.message)
      }
    },
    onNegativeClick: () => {
      message.info('已取消排队，可重新点击「创建实例」')
    },
  })
}

const submitTask = async (submittedName: string) => {
  launching.value = true
  // The record exists a moment after the request starts: show it while OCI is working.
  window.setTimeout(() => {
    if (launching.value) fetchTasks()
  }, 3000)
  try {
    const res: any = await api.post(
      '/tasks/create',
      {
        profile_id: profileStore.activeProfileId,
        instance_name: submittedName,
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
        open_all_ports: form.value.open_all_ports,
      },
      { timeout: 200000 },
    )
    // The response settles this task; polling must not announce it a second time.
    if (watchedTaskId.value === res.task_id) {
      watchedTaskId.value = ''
      watchedPrevStatus.value = ''
    }
    await fetchTasks()

    if (res.result === 'created') {
      dialog.success({
        title: res.existed ? '实例已存在' : '实例创建成功',
        content: `${submittedName} 已在 OCI 创建成功${res.existed ? '（云端已有同名实例）' : '，已返回实例 OCID'}。公网 IP 分配后将在「实例」页显示。`,
        positiveText: '确定',
      })
      form.value.instance_name = randomInstanceName()
      if (form.value.login_mode === 'root_password') generateRandomPassword()
    } else if (res.retryable) {
      askAutoRetry(res)
    } else {
      dialog.error({ title: '创建失败', content: res.reason || '未知错误', positiveText: '确定' })
    }
  } catch (e: any) {
    // No answer (timeout, proxy limit): the backend keeps going and writes the result to the
    // record; polling picks it up and announces it here.
    dialog.warning({
      title: '暂未收到创建结果',
      content: `与服务器的连接超时（${e.message}）。创建可能仍在后台进行，本页将持续刷新并提示结果，也可在「实例」页查看。`,
      positiveText: '确定',
    })
    fetchTasks()
  } finally {
    launching.value = false
  }
}

const confirmAndCreate = () => {
  const name = form.value.instance_name.trim() || randomInstanceName()
  const imageLabel = imageOptions.value.find((o) => o.value === form.value.image_ocid)?.label || form.value.image_ocid
  const subnetLabel = subnetOptions.value.find((o) => o.value === form.value.subnet_ocid)?.label || form.value.subnet_ocid
  const rows: [string, string][] = [
    ['账号', `${currentProfile.value?.name || ''} · ${regionLabel(currentProfile.value?.region)}`],
    ['实例名称', name],
    ['规格', form.value.shape.includes('A1') ? `ARM A1 Flex · ${form.value.ocpu} OCPU / ${form.value.memory_in_gbs} GB` : 'AMD E2.1 Micro · 1 OCPU / 1 GB'],
    ['镜像', imageLabel],
    ['可用区', form.value.ad_list.length ? form.value.ad_list.map(shortAD).join('、') : '依次尝试全部可用区'],
    ['引导卷', `${form.value.boot_volume_size_in_gbs} GB · ${form.value.boot_volume_vpu} VPU`],
    ['子网', subnetLabel],
    ['公网地址', `${form.value.assign_public_ip ? 'IPv4' : '不分配 IPv4'}${form.value.enable_ipv6 ? ' + IPv6' : ''}`],
    ['防火墙', form.value.open_all_ports ? '专属防火墙，开放全部端口（检测到 IPv6 时一并放通）' : '不自动配置，创建后在「实例」页设置'],
    ['登录方式', form.value.login_mode === 'root_key' ? `root + SSH 密钥${form.value.ssh_authorized_keys.trim() ? '' : '（未填写公钥）'}` : 'root + 随机密码（写入云端标签）'],
  ]
  dialog.info({
    title: '确认创建实例',
    style: { width: '560px', maxWidth: '95vw' },
    content: () =>
      h(
        'dl',
        { class: 'grid grid-cols-[6em_1fr] gap-x-3 gap-y-1.5 text-[13px] leading-5' },
        rows.flatMap(([k, v]) => [
          h('dt', { class: 'text-ink-3' }, k),
          h('dd', { class: 'mono break-all text-ink' }, v),
        ]),
      ),
    positiveText: '确认创建',
    negativeText: '返回修改',
    onPositiveClick: () => submitTask(name),
  })
}

const handleCreateTask = () => {
  if (!profileStore.activeProfileId) {
    message.error('请先选择一个 OCI 账号')
    return
  }
  if (!form.value.image_ocid || !form.value.subnet_ocid) {
    message.warning('请选择镜像和子网')
    return
  }
  if (form.value.login_mode === 'root_key' && !form.value.ssh_authorized_keys.trim()) {
    dialog.warning({
      title: '未填写 SSH 公钥',
      content: '密钥登录模式下未填写公钥，创建的实例将无法登录。是否继续？',
      positiveText: '继续',
      negativeText: '返回填写',
      onPositiveClick: confirmAndCreate,
    })
    return
  }
  if (form.value.login_mode === 'root_password' && !form.value.root_password) generateRandomPassword()
  confirmAndCreate()
}

const startPolling = () => {
  if (pollTimer) window.clearInterval(pollTimer)
  pollTimer = window.setInterval(() => {
    if (launching.value || tasks.value.some(isActiveTask)) fetchTasks()
  }, 10000)
}

const loadForProfile = async () => {
  form.value.region = currentProfile.value?.region || ''
  form.value.image_ocid = ''
  form.value.ad_list = []
  netsLoaded.value = false
  await Promise.all([loadPresets(), loadImages(), loadNetworks(), loadADs(), fetchTasks(), loadSSHKeys()])
}

watch(
  () => profileStore.activeProfileId,
  () => {
    loadForProfile()
  },
)

onMounted(async () => {
  generateRandomPassword()
  await loadForProfile()
  startPolling()
})

onBeforeUnmount(() => {
  if (pollTimer) window.clearInterval(pollTimer)
})
</script>
