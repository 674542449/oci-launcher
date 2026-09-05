<template>
  <div>
    <PageHeader title="创建实例" description="填写规格后立即创建。容量不足时可以加入排队，系统每 60–180 秒随机重试一次，直到创建成功。">
      <template #actions>
        <n-button secondary :loading="vcnCreating" :disabled="!currentProfile" @click="handleCreateDefaultVCN">
          <template #icon><n-icon><GlobeOutline /></n-icon></template>
          一键创建推荐 VCN
        </n-button>
      </template>
    </PageHeader>

    <!-- ===== Presets ===== -->
    <section v-if="presets.length" class="mb-6">
      <div class="mb-2 flex items-baseline justify-between">
        <h2 class="section-title">免费额度预设</h2>
        <span class="caption">点击填入下方表单</span>
      </div>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <button
          v-for="preset in presets"
          :key="preset.id"
          type="button"
          class="card p-3.5 text-left transition-all hover:border-line-strong hover:shadow-card"
          :class="selectedPresetId === preset.id ? '!border-brand ring-2 ring-brand/20' : ''"
          @click="applyPreset(preset)"
        >
          <div class="truncate text-[13px] font-semibold text-ink">{{ preset.name }}</div>
          <div class="mono mt-1 text-xs text-ink-2">{{ preset.shape.includes('A1') ? 'ARM A1' : 'AMD E2 Micro' }} · {{ preset.ocpu }}C / {{ preset.memory_in_gbs }}G</div>
          <div class="mono mt-0.5 text-xs text-ink-3">{{ preset.boot_volume_size_in_gbs }} GB · {{ preset.boot_volume_vpu || 120 }} VPU<span v-if="preset.enable_ipv6"> · IPv6</span></div>
        </button>
      </div>
    </section>

    <div class="grid grid-cols-1 gap-6 xl:grid-cols-5">
      <!-- ===== Form ===== -->
      <section class="card card-pad xl:col-span-3">
        <n-form label-placement="top" :show-feedback="false" @submit.prevent="handleCreateTask">
          <div class="space-y-7">
            <!-- 规格 -->
            <fieldset class="space-y-4">
              <legend class="mb-3 text-xs font-semibold uppercase tracking-wider text-ink-3">规格</legend>
              <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <n-form-item label="实例名称">
                  <n-input v-model:value="form.instance_name" placeholder="留空则自动生成" :input-props="{ spellcheck: 'false' }" class="mono">
                    <template #suffix>
                      <button type="button" class="txt-btn-muted" title="换一个随机名称" @click="form.instance_name = randomInstanceName()">换一个</button>
                    </template>
                  </n-input>
                </n-form-item>
                <n-form-item label="Shape">
                  <n-select v-model:value="form.shape" :options="shapeOptions" @update:value="onShapeChange" />
                </n-form-item>
              </div>
              <p class="caption -mt-2">名称随机生成，同名实例已存在时不会重复创建。</p>

              <div v-if="isA1" class="grid grid-cols-1 gap-5 rounded-lg border border-line bg-surface-2 p-4 sm:grid-cols-2">
                <div>
                  <div class="mb-1.5 flex items-center justify-between">
                    <span class="label mb-0">OCPU</span>
                    <span class="mono text-sm font-semibold text-ink">{{ form.ocpu }} 核</span>
                  </div>
                  <n-slider v-model:value="form.ocpu" :min="1" :max="4" :step="1" :marks="{ 1: '1', 2: '2', 3: '3', 4: '4' }" />
                </div>
                <div>
                  <div class="mb-1.5 flex items-center justify-between">
                    <span class="label mb-0">内存</span>
                    <span class="mono text-sm font-semibold text-ink">{{ form.memory_in_gbs }} GB</span>
                  </div>
                  <n-slider v-model:value="form.memory_in_gbs" :min="1" :max="24" :step="1" :marks="{ 6: '6', 12: '12', 18: '18', 24: '24' }" />
                </div>
                <p class="caption sm:col-span-2">免费号最多 2 OCPU / 12 GB，升级号最多 4 OCPU / 24 GB；每 OCPU 建议搭配 6 GB 内存。</p>
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
              <p class="caption -mt-2">镜像按 Shape 架构自动筛选：A1 用 aarch64，E2 Micro 用 x86_64。</p>
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
              <p class="caption -mt-2">引导卷与块存储合计免费 200 GB。高于 10 VPU 的档位在已升级账号上会按 VPU 计费。</p>
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
                <n-checkbox v-model:checked="form.enable_ipv6">分配 IPv6 并放通防火墙</n-checkbox>
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
                </div>
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
              <span class="caption">点击后立即向 OCI 发起创建；同一时间只允许操作一个账号，创建前会校验免费额度。</span>
              <n-button type="primary" size="large" attr-type="submit" :loading="launching" :disabled="!currentProfile">
                <template #icon><n-icon><RocketOutline /></n-icon></template>
                {{ launching ? '正在创建…' : '创建实例' }}
              </n-button>
            </div>
          </div>
        </n-form>
      </section>

      <!-- ===== Live log ===== -->
      <section ref="terminalSection" class="term flex min-h-[440px] flex-col rounded-xl xl:sticky xl:top-8 xl:col-span-2 xl:max-h-[calc(100vh-4rem)]">
        <div class="flex items-center justify-between gap-3 border-b border-side-2 px-4 py-3">
          <div class="flex min-w-0 items-center gap-2.5">
            <span class="relative flex h-2.5 w-2.5 shrink-0">
              <span v-if="wsConnected" class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60"></span>
              <span class="relative inline-flex h-2.5 w-2.5 rounded-full" :class="wsConnected ? 'bg-emerald-400' : 'bg-side-3'"></span>
            </span>
            <span class="text-[13px] font-semibold">实时日志</span>
            <span v-if="activeTask" class="mono truncate text-xs text-side-muted">{{ activeTask.instance_name }}</span>
          </div>
          <button type="button" class="text-xs text-side-muted transition-colors hover:text-side-ink" :disabled="logMessages.length === 0" @click="logMessages = []">清空</button>
        </div>

        <div ref="terminalBody" class="mono flex-1 overflow-y-auto px-3 py-2 text-xs" aria-live="polite">
          <div v-if="logMessages.length === 0" class="flex h-full flex-col items-center justify-center gap-1 py-16 text-center text-side-muted">
            <n-icon size="22" class="mb-1 opacity-60"><TerminalOutline /></n-icon>
            <span>{{ activeTask ? '等待新的记录…' : '创建实例后，每次尝试的结果会实时显示在这里。' }}</span>
            <span class="text-[11px] opacity-70">也可以在下方创建记录点「查看日志」</span>
          </div>
          <ol v-else class="space-y-1">
            <li v-for="(msg, idx) in logMessages" :key="idx" class="rounded-md px-2 py-1.5 leading-5 hover:bg-side-2/70">
              <div class="flex flex-wrap items-center gap-x-2 text-side-muted">
                <span>{{ formatLogTime(msg.timestamp) }}</span>
                <span v-if="msg.attempt_num">#{{ msg.attempt_num }}</span>
                <span v-if="msg.ad" class="truncate">{{ shortAD(msg.ad) }}</span>
                <span v-if="msg.duration_ms" class="ml-auto">{{ msg.duration_ms }} ms</span>
              </div>
              <div class="mt-0.5 flex items-start gap-2">
                <span class="mt-[3px] h-1.5 w-1.5 shrink-0 rounded-full" :class="logDot(msg.status)"></span>
                <span class="break-words" :class="logText(msg.status)">{{ msg.message }}</span>
              </div>
            </li>
          </ol>
        </div>

        <div class="flex items-center justify-between border-t border-side-2 px-4 py-2 text-[11px] text-side-muted">
          <span>{{ wsConnected ? '已连接' : '未连接' }}</span>
          <span>{{ logMessages.length }} 条</span>
        </div>
      </section>
    </div>

    <!-- ===== Records ===== -->
    <section class="card mt-6 overflow-hidden">
      <div class="card-head card-pad pb-4">
        <div>
          <h2 class="section-title">创建记录</h2>
          <p class="caption">当前账号的创建与排队记录。排队中的任务每 15 秒自动刷新。</p>
        </div>
        <n-button size="small" secondary :loading="loadingTasks" @click="fetchTasks">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新
        </n-button>
      </div>
      <EmptyState v-if="!loadingTasks && tasks.length === 0" title="还没有创建记录" description="用上方表单创建第一台实例。" />
      <div v-else class="tbl-wrap border-t border-line">
        <table class="tbl">
          <thead>
            <tr>
              <th>实例</th>
              <th>状态</th>
              <th>尝试</th>
              <th>最近消息</th>
              <th>结果</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in tasks" :key="t.id" :class="activeTask?.id === t.id ? 'bg-surface-2/70' : ''">
              <td class="min-w-[200px]">
                <div class="mono font-semibold text-ink">{{ t.instance_name }}</div>
                <div class="mono mt-0.5 text-xs text-ink-3">{{ t.shape.includes('A1') ? 'A1' : 'E2 Micro' }} · {{ t.ocpu }}C / {{ t.memory_in_gbs }}G · {{ t.boot_volume_size_in_gbs }} GB</div>
                <div class="mono mt-0.5 text-xs text-ink-3">{{ formatTime(t.created_at) }}</div>
              </td>
              <td><StatusPill :state="taskState(t.status)" :label="taskLabel(t.status)" /></td>
              <td class="mono whitespace-nowrap text-ink">{{ t.current_retries }}<span v-if="t.max_retries" class="text-ink-3"> / {{ t.max_retries }}</span></td>
              <td class="max-w-[280px]"><span class="line-clamp-2 text-xs leading-5 text-ink-2" :title="t.last_message">{{ t.last_message || '—' }}</span></td>
              <td class="whitespace-nowrap">
                <div v-if="t.success_public_ip" class="mono text-[13px] font-medium text-ink">{{ t.success_public_ip }}</div>
                <div v-if="t.success_ipv6" class="mono max-w-[180px] truncate text-xs text-ink-3" :title="t.success_ipv6">{{ t.success_ipv6 }}</div>
                <span v-if="!t.success_public_ip && !t.success_ipv6" class="text-xs text-ink-3">—</span>
              </td>
              <td class="text-right whitespace-nowrap">
                <div class="inline-flex items-center gap-1.5">
                  <n-button size="small" secondary @click="viewLogs(t)">查看日志</n-button>
                  <n-button v-if="t.status === 'running'" size="small" secondary type="warning" :loading="taskActing === t.id" @click="stopTask(t)">停止排队</n-button>
                  <n-button v-else-if="t.status === 'stopped' || t.status === 'failed'" size="small" secondary type="success" :loading="taskActing === t.id" @click="startTask(t)">排队重试</n-button>
                  <n-button size="small" quaternary type="error" @click="deleteTask(t)">删除</n-button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import {
  NButton,
  NIcon,
  NForm,
  NFormItem,
  NInput,
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
import { GlobeOutline, RefreshOutline, CopyOutline, RocketOutline, TerminalOutline, CloudUploadOutline } from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import StatusPill from '@/components/StatusPill.vue'
import EmptyState from '@/components/EmptyState.vue'

const profileStore = useProfileStore()
const message = useMessage()
const dialog = useDialog()

const launching = ref(false)
const vcnCreating = ref(false)
const loadingImages = ref(false)
const loadingNets = ref(false)
const loadingADs = ref(false)
const presets = ref<any[]>([])
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
const activeTask = ref<any>(null)

const wsConnected = ref(false)
const logMessages = ref<any[]>([])
const terminalBody = ref<HTMLElement | null>(null)
const terminalSection = ref<HTMLElement | null>(null)
let socket: WebSocket | null = null
let pollTimer: number | null = null

// Project-style random names: adjective-noun-NN
const ADJECTIVES = ['amber', 'brisk', 'calm', 'coral', 'crisp', 'dusk', 'ember', 'fern', 'frost', 'gale', 'glen', 'hazel', 'ivory', 'jade', 'lunar', 'maple', 'misty', 'nova', 'ocean', 'onyx', 'opal', 'pearl', 'pine', 'quiet', 'river', 'sage', 'slate', 'solar', 'storm', 'swift', 'terra', 'tidal', 'vivid', 'willow', 'zephyr']
const NOUNS = ['atlas', 'beacon', 'cedar', 'comet', 'delta', 'falcon', 'harbor', 'heron', 'iris', 'kestrel', 'lantern', 'lotus', 'meadow', 'nimbus', 'orbit', 'osprey', 'pixel', 'quartz', 'ridge', 'sparrow', 'summit', 'tundra', 'vector', 'vertex', 'voyager', 'zenith']
const pick = (arr: string[]) => arr[Math.floor(Math.random() * arr.length)]
const randomInstanceName = () => `${pick(ADJECTIVES)}-${pick(NOUNS)}-${String(Math.floor(Math.random() * 100)).padStart(2, '0')}`

const form = ref({
  instance_name: randomInstanceName(),
  shape: 'VM.Standard.A1.Flex',
  ocpu: 2,
  memory_in_gbs: 12,
  boot_volume_size_in_gbs: 50,
  boot_volume_vpu: 120,
  region: '',
  ad_list: [] as string[],
  image_ocid: '',
  subnet_ocid: '',
  login_mode: 'root_key',
  ssh_authorized_keys: '',
  root_password: '',
  assign_public_ip: true,
  enable_ipv6: true,
})

const shapeOptions = [
  { label: 'ARM Ampere A1 Flex（可调核数与内存）', value: 'VM.Standard.A1.Flex' },
  { label: 'AMD E2.1 Micro（1 OCPU / 1 GB）', value: 'VM.Standard.E2.1.Micro' },
]

const vpuOptions = [
  { label: '120 VPU · 超高性能', value: 120 },
  { label: '60 VPU · 超高性能', value: 60 },
  { label: '20 VPU · 高性能', value: 20 },
  { label: '10 VPU · 均衡（引导卷最低档）', value: 10 },
]

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))
const isA1 = computed(() => form.value.shape.includes('A1'))

const shortAD = (ad?: string) => (ad ? ad.replace(/^[^:]+:/, '') : '')
const formatTime = (t: string) => (t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '')
const formatLogTime = (t: string) => {
  if (!t) return ''
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? t : d.toLocaleTimeString('zh-CN', { hour12: false })
}

const taskState = (s: string) => ({ running: 'RUNNING_TASK', creating: 'PROVISIONING', success: 'SUCCESS', stopped: 'STOPPED', failed: 'FAILED', idle: 'IDLE' })[s] || s
const taskLabel = (s: string) => ({ running: '排队重试中', creating: '创建中', success: '已创建', stopped: '未排队', failed: '失败', idle: '空闲' })[s] || s

const logDot = (s: string) =>
  ({ SUCCESS: 'bg-emerald-400', FATAL: 'bg-red-400', RATE_LIMIT: 'bg-amber-400', STOPPED: 'bg-side-muted', WAIT: 'bg-side-muted' })[s] || 'bg-side-3'
const logText = (s: string) =>
  ({ SUCCESS: 'text-emerald-300 font-semibold', FATAL: 'text-red-300 font-semibold', RATE_LIMIT: 'text-amber-200', STOPPED: 'text-side-muted', WAIT: 'text-side-muted' })[s] || 'text-side-ink'

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
      message.warning('这个文件看起来不是 SSH 公钥（应以 ssh-ed25519 或 ssh-rsa 开头）。请选择 .pub 文件，而不是私钥。')
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

const applyPreset = (preset: any) => {
  selectedPresetId.value = preset.id
  form.value.shape = preset.shape
  form.value.ocpu = preset.ocpu
  form.value.memory_in_gbs = preset.memory_in_gbs
  form.value.boot_volume_size_in_gbs = preset.boot_volume_size_in_gbs
  form.value.boot_volume_vpu = preset.boot_volume_vpu || 120
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
    form.value.ocpu = 2
    form.value.memory_in_gbs = 12
  }
  form.value.image_ocid = ''
  loadImages()
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
    vcnOptions.value = (res.vcns || []).map((v: any) => ({ label: `${v.display_name} (${v.cidr_block})`, value: v.ocid }))
    if (vcnOptions.value.length > 0) {
      selectedVCN.value = vcnOptions.value[0].value
      await onVCNChange()
    } else {
      selectedVCN.value = null
      subnetOptions.value = []
      form.value.subnet_ocid = ''
    }
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

// ---------- live log ----------
const closeSocket = () => {
  if (socket) {
    socket.onclose = null
    socket.close()
    socket = null
  }
  wsConnected.value = false
}

const connectWebSocket = (taskID: string) => {
  closeSocket()
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  socket = new WebSocket(`${protocol}//${window.location.host}/ws/logs/${taskID}`)
  socket.onopen = () => (wsConnected.value = true)
  socket.onmessage = (event) => {
    try {
      const parsed = JSON.parse(event.data)
      logMessages.value.unshift(parsed)
      if (logMessages.value.length > 500) logMessages.value.length = 500
      nextTick(() => {
        if (terminalBody.value) terminalBody.value.scrollTop = 0
      })
      if (['SUCCESS', 'FATAL', 'STOPPED'].includes(parsed.status)) fetchTasks()
    } catch {
      /* ignore malformed frames */
    }
  }
  socket.onclose = () => (wsConnected.value = false)
}

const attemptToLog = (a: any) => ({
  task_id: a.task_id,
  attempt_num: a.attempt_num,
  timestamp: a.created_at,
  region: a.region,
  ad: a.ad,
  status: ({ success: 'SUCCESS', fatal_error: 'FATAL', rate_limited: 'RATE_LIMIT', capacity_full: 'RETRY', transient_error: 'RETRY' } as any)[a.status] || (a.status || '').toUpperCase(),
  message: a.response_message,
  duration_ms: a.duration_ms,
})

const viewLogs = async (task: any) => {
  activeTask.value = task
  logMessages.value = []
  try {
    const res: any = await api.get(`/tasks/attempts/${task.id}`)
    logMessages.value = (res.attempts || []).map(attemptToLog)
  } catch {
    /* history is optional */
  }
  connectWebSocket(task.id)
  if (window.innerWidth < 1280) terminalSection.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

// ---------- records ----------
const fetchTasks = async () => {
  if (!profileStore.activeProfileId) return
  loadingTasks.value = true
  try {
    const res: any = await api.get(`/tasks?profile_id=${profileStore.activeProfileId}`)
    tasks.value = res.tasks || []
    if (activeTask.value) {
      const fresh = tasks.value.find((t) => t.id === activeTask.value.id)
      if (fresh) activeTask.value = fresh
    }
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loadingTasks.value = false
  }
}

const startTask = async (t: any) => {
  taskActing.value = t.id
  try {
    const res: any = await api.post(`/tasks/start/${t.id}`)
    message.success(res.message || '已加入排队')
    await fetchTasks()
    viewLogs(t)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    taskActing.value = ''
  }
}

const stopTask = async (t: any) => {
  taskActing.value = t.id
  try {
    const res: any = await api.post(`/tasks/stop/${t.id}`)
    message.success(res.message || '已停止排队')
    await fetchTasks()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    taskActing.value = ''
  }
}

const deleteTask = (t: any) => {
  dialog.error({
    title: '删除记录',
    content: `删除 ${t.instance_name} 的创建记录及其尝试历史？排队中的任务会先被停止。已创建的实例不受影响。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await api.delete(`/tasks/delete/${t.id}`)
        if (activeTask.value?.id === t.id) {
          activeTask.value = null
          closeSocket()
          logMessages.value = []
        }
        message.success('记录已删除')
        await fetchTasks()
      } catch (e: any) {
        message.error(e.message)
      }
    },
  })
}

// ---------- create ----------
const askAutoRetry = (res: any) => {
  dialog.warning({
    title: '创建失败，是否自动重试？',
    content: `${res.reason || '容量不足'}。已尝试 ${res.attempts || 1} 个可用区。加入排队后系统每 60–180 秒随机重试一次，直到创建成功或你手动停止。`,
    positiveText: '自动重试',
    negativeText: '不重试',
    onPositiveClick: async () => {
      try {
        const start: any = await api.post(`/tasks/start/${res.task_id}`)
        message.success(start.message || '已加入排队')
        await fetchTasks()
        const t = tasks.value.find((x) => x.id === res.task_id) || res.task
        if (t) viewLogs(t)
      } catch (e: any) {
        message.error(e.message)
      }
    },
    onNegativeClick: () => {
      message.info('已保留创建记录，可随时在下方点「排队重试」')
    },
  })
}

const submitTask = async () => {
  launching.value = true
  const submittedName = form.value.instance_name.trim() || randomInstanceName()
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
      },
      { timeout: 160000 },
    )
    await fetchTasks()
    const created = res.task || tasks.value.find((t) => t.id === res.task_id)
    if (created) {
      activeTask.value = created
      logMessages.value = []
      connectWebSocket(created.id)
    }

    if (res.result === 'created') {
      dialog.success({
        title: res.existed ? '实例已存在' : '实例创建成功',
        content: `${submittedName} 已在 OCI 创建${res.existed ? '（云端已有同名实例）' : ''}。正在等待公网 IP 分配，完成后会显示在创建记录和「实例」页面。`,
        positiveText: '知道了',
      })
      form.value.instance_name = randomInstanceName()
      if (form.value.login_mode === 'root_password') generateRandomPassword()
    } else if (res.retryable) {
      askAutoRetry(res)
    } else {
      dialog.error({
        title: '创建失败',
        content: res.reason || '未知错误',
        positiveText: '知道了',
      })
    }
  } catch (e: any) {
    message.error(e.message)
  } finally {
    launching.value = false
  }
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
      title: '没有填写 SSH 公钥',
      content: '密钥模式下不填公钥，创建出的实例将无法登录。仍要继续吗？',
      positiveText: '继续创建',
      negativeText: '返回填写',
      onPositiveClick: submitTask,
    })
    return
  }
  if (form.value.login_mode === 'root_password' && !form.value.root_password) generateRandomPassword()
  submitTask()
}

const loadPresets = async () => {
  try {
    const res: any = await api.get('/tasks/presets')
    presets.value = res.presets || []
  } catch {
    presets.value = []
  }
}

const startPolling = () => {
  if (pollTimer) window.clearInterval(pollTimer)
  pollTimer = window.setInterval(() => {
    if (tasks.value.some((t) => t.status === 'running' || t.status === 'creating')) fetchTasks()
  }, 15000)
}

const loadForProfile = async () => {
  form.value.region = currentProfile.value?.region || ''
  form.value.image_ocid = ''
  form.value.ad_list = []
  activeTask.value = null
  closeSocket()
  logMessages.value = []
  await Promise.all([loadImages(), loadNetworks(), loadADs(), fetchTasks()])
}

watch(
  () => profileStore.activeProfileId,
  () => {
    loadForProfile()
  },
)

onMounted(async () => {
  generateRandomPassword()
  await loadPresets()
  await loadForProfile()
  startPolling()
})

onBeforeUnmount(() => {
  closeSocket()
  if (pollTimer) window.clearInterval(pollTimer)
})
</script>
