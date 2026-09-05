<template>
  <div>
    <PageHeader title="账号" description="导入 OCI API 凭据，为每个账号打标签、写备注，并单独做健康体检。私钥加密后存储。">
      <template #actions>
        <n-button secondary :loading="syncing" @click="handleSyncLocal">
          <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
          同步宿主机配置
        </n-button>
        <n-button type="primary" @click="showImportModal = true">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          导入账号
        </n-button>
      </template>
    </PageHeader>

    <!-- Search -->
    <div v-if="profileStore.profiles.length > 0" class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <n-input v-model:value="searchKeyword" placeholder="按别名、区域、标签或备注筛选" clearable class="sm:max-w-xs">
        <template #prefix><n-icon class="text-ink-3"><SearchOutline /></n-icon></template>
      </n-input>
      <span class="caption">共 {{ profileStore.profiles.length }} 个账号<template v-if="searchKeyword">，匹配 {{ filteredProfiles.length }} 个</template></span>
    </div>

    <!-- Empty -->
    <div v-if="!profileStore.loading && profileStore.profiles.length === 0" class="card">
      <EmptyState title="还没有导入任何账号" description="在 Oracle 控制台「添加 API 密钥」后，把生成的配置块和私钥粘贴进来即可。">
        <n-button type="primary" @click="showImportModal = true">导入第一个账号</n-button>
      </EmptyState>
    </div>
    <div v-else-if="searchKeyword && filteredProfiles.length === 0" class="card">
      <EmptyState title="没有匹配的账号" description="换个关键字试试。" />
    </div>

    <!-- Cards -->
    <div v-else class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
      <article
        v-for="p in filteredProfiles"
        :key="p.id"
        class="card card-pad relative flex flex-col gap-4 transition-shadow"
        :class="p.id === profileStore.activeProfileId ? 'ring-2 ring-brand/30 border-brand/60' : 'hover:shadow-card'"
      >
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <span class="text-xl leading-none" :title="regionCountry(p.region)" aria-hidden="true">{{ regionFlag(p.region) }}</span>
              <h2 class="truncate text-[15px] font-semibold text-ink">{{ p.name }}</h2>
              <span v-if="p.id === profileStore.activeProfileId" class="pill pill-info">当前</span>
            </div>
            <div class="mt-0.5 text-xs text-ink-2">
              {{ regionLabel(p.region) }}
              <span class="mono text-ink-3">· {{ p.region }}</span>
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-end gap-1">
            <StatusPill :state="p.status" :label="statusLabel(p.status)" />
            <span class="pill" :class="accountTypeClass(p)" :title="p.detection_reason || ''">{{ accountTypeLabel(p) }}</span>
          </div>
        </div>

        <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs leading-5">
          <dt class="text-ink-3">邮箱</dt>
          <dd class="mono truncate text-ink" :title="p.account_email">{{ p.account_email || '体检后自动获取' }}</dd>
          <dt class="text-ink-3">注册</dt>
          <dd class="mono text-ink">{{ formatDate(p.account_created_at) }}<span v-if="countryOf(p)" class="text-ink-3"> · {{ countryOf(p) }}</span></dd>
          <dt v-if="p.tenancy_name" class="text-ink-3">租户</dt>
          <dd v-if="p.tenancy_name" class="mono truncate text-ink" :title="p.tenancy_name">{{ p.tenancy_name }}</dd>
        </dl>

        <div v-if="p.tags" class="flex flex-wrap gap-1.5">
          <span v-for="t in splitTags(p.tags)" :key="t" class="rounded-md border border-line bg-surface-2 px-2 py-0.5 text-xs text-ink-2">{{ t }}</span>
        </div>

        <p v-if="p.notes" class="rounded-lg border border-line bg-surface-2 px-3 py-2 text-xs leading-5 text-ink-2">{{ p.notes }}</p>
        <p v-if="p.status !== 'Active' && p.status_message" class="text-xs leading-5 text-danger">{{ p.status_message }}</p>

        <dl class="mono space-y-1 border-t border-line pt-3 text-xs text-ink-3">
          <div class="flex gap-2"><dt class="shrink-0">租户</dt><dd class="truncate text-ink-2" :title="p.tenancy_ocid">{{ maskOCID(p.tenancy_ocid) }}</dd></div>
          <div class="flex gap-2"><dt class="shrink-0">指纹</dt><dd class="truncate text-ink-2" :title="p.fingerprint">{{ p.fingerprint }}</dd></div>
        </dl>

        <div class="mt-auto flex items-center justify-between gap-2 border-t border-line pt-3">
          <n-button v-if="p.id !== profileStore.activeProfileId" size="small" type="primary" secondary @click="profileStore.setActiveProfile(p.id)">设为当前账号</n-button>
          <span v-else class="text-xs font-medium text-ink-3">正在操作此账号</span>
          <div class="flex items-center gap-1">
            <n-button size="small" quaternary :loading="checkingHealthId === p.id" @click="checkHealth(p)">体检</n-button>
            <n-button size="small" quaternary @click="openEditProfileModal(p)">编辑</n-button>
            <n-button size="small" quaternary type="error" @click="deleteProfile(p)">删除</n-button>
          </div>
        </div>
      </article>
    </div>

    <!-- Import -->
    <n-modal v-model:show="showImportModal" preset="card" title="导入 OCI 账号" style="max-width: 640px" :bordered="false">
      <div class="space-y-5">
        <div class="notice notice-info">
          <n-icon size="18" class="mt-0.5 shrink-0"><InformationCircleOutline /></n-icon>
          <span>在 Oracle 控制台 → 用户 → API 密钥 → 添加 API 密钥，把生成的「配置文件预览」整段粘贴到下面，系统会自动解析 user、tenancy、fingerprint 和 region。</span>
        </div>

        <div>
          <span class="label">配置块</span>
          <n-input
            v-model:value="importForm.raw_config"
            type="textarea"
            class="mono"
            placeholder="[DEFAULT]&#10;user=ocid1.user.oc1..aaaa…&#10;fingerprint=12:34:56:…&#10;tenancy=ocid1.tenancy.oc1..aaaa…&#10;region=ap-tokyo-1"
            :rows="6"
            :input-props="{ spellcheck: 'false' }"
          />
        </div>

        <div class="rounded-lg border border-line bg-surface-2 p-4">
          <span class="label">API 私钥（三选一）</span>
          <n-tabs v-model:value="keyTab" type="segment" size="small" animated>
            <n-tab-pane name="file" tab="选择 .pem 文件">
              <label class="mt-3 flex cursor-pointer items-center justify-center gap-2 rounded-lg border border-dashed border-line-strong bg-surface px-4 py-5 text-[13px] text-ink-2 transition-colors hover:border-brand hover:text-ink">
                <n-icon size="18"><CloudUploadOutline /></n-icon>
                <span>{{ keyFileName || '点击选择私钥文件（.pem / .key）' }}</span>
                <input type="file" accept=".pem,.key" class="sr-only" @change="onFileSelected" />
              </label>
            </n-tab-pane>
            <n-tab-pane name="text" tab="粘贴 PEM">
              <n-input
                v-model:value="importForm.private_key_pem"
                type="textarea"
                class="mono mt-3"
                placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;…&#10;-----END RSA PRIVATE KEY-----"
                :rows="5"
                :input-props="{ spellcheck: 'false' }"
              />
            </n-tab-pane>
            <n-tab-pane name="path" tab="宿主机路径">
              <n-input v-model:value="importForm.key_file_path" class="mono mt-3" placeholder="~/.oci/oci_api_key.pem" />
              <p class="caption mt-1">路径相对于后端容器内挂载的 /root/.oci。</p>
            </n-tab-pane>
          </n-tabs>
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <span class="label">标签（逗号分隔）</span>
            <n-input v-model:value="importForm.tags" placeholder="主力号, 已升级, 东京" />
          </div>
          <div>
            <span class="label">备注</span>
            <n-input v-model:value="importForm.notes" placeholder="注册邮箱、卡号后四位等" />
          </div>
        </div>

        <div class="flex justify-end gap-2 border-t border-line pt-4">
          <n-button @click="showImportModal = false">取消</n-button>
          <n-button type="primary" :loading="importing" @click="submitImport">导入并验证连通性</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Edit -->
    <n-modal v-model:show="showEditModal" preset="card" title="编辑账号标签与备注" style="max-width: 460px" :bordered="false">
      <div v-if="editingProfile" class="space-y-4">
        <div>
          <span class="label">标签（逗号分隔）</span>
          <n-input v-model:value="editForm.tags" placeholder="主力号, 已升级, 东京" />
        </div>
        <div>
          <span class="label">备注</span>
          <n-input v-model:value="editForm.notes" type="textarea" :rows="3" placeholder="备忘信息" />
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <n-button @click="showEditModal = false">取消</n-button>
          <n-button type="primary" :loading="savingEdit" @click="submitEditProfile">保存</n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { NButton, NIcon, NInput, NModal, NTabs, NTabPane, useMessage, useDialog } from 'naive-ui'
import { AddOutline, FolderOpenOutline, SearchOutline, InformationCircleOutline, CloudUploadOutline } from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import type { OCIProfile } from '@/stores/profile'
import { api } from '@/api/client'
import { regionLabel, regionCountry, regionFlag, countryName } from '@/lib/regions'
import PageHeader from '@/components/PageHeader.vue'
import StatusPill from '@/components/StatusPill.vue'
import EmptyState from '@/components/EmptyState.vue'

const profileStore = useProfileStore()
const message = useMessage()
const dialog = useDialog()

const searchKeyword = ref('')
const showImportModal = ref(false)
const importing = ref(false)
const syncing = ref(false)
const checkingHealthId = ref<number | null>(null)
const keyTab = ref('file')
const keyFileName = ref('')

const showEditModal = ref(false)
const editingProfile = ref<OCIProfile | null>(null)
const editForm = ref({ tags: '', notes: '' })
const savingEdit = ref(false)

const importForm = ref({ raw_config: '', private_key_pem: '', key_file_path: '', tags: '', notes: '' })

const filteredProfiles = computed(() => {
  if (!searchKeyword.value) return profileStore.profiles
  const kw = searchKeyword.value.toLowerCase()
  return profileStore.profiles.filter(
    (p) =>
      p.name.toLowerCase().includes(kw) ||
      p.region.toLowerCase().includes(kw) ||
      (p.tags && p.tags.toLowerCase().includes(kw)) ||
      (p.notes && p.notes.toLowerCase().includes(kw)),
  )
})

const statusLabel = (s: string) => (s === 'Active' ? '正常' : s === 'Banned' ? '疑似封号' : s === 'Invalid' ? '凭据无效' : s || '未知')

// Account type as reported by the Organizations subscription API (or the manual override)
const accountTypeLabel = (p: OCIProfile) => {
  const o = (p.account_type_override || 'auto').toLowerCase()
  if (o === 'payg') return '升级号 · 手动'
  if (o === 'free') return '免费号 · 手动'
  if (p.detected_type === 'PAYG') return '升级号'
  if (p.detected_type === 'FREE_TIER') return '免费号'
  return '类型未判定'
}
const accountTypeClass = (p: OCIProfile) => {
  const o = (p.account_type_override || 'auto').toLowerCase()
  const paid = o === 'payg' || (o === 'auto' && p.detected_type === 'PAYG')
  const free = o === 'free' || (o === 'auto' && p.detected_type === 'FREE_TIER')
  return paid ? 'pill-info' : free ? 'pill-ok' : 'pill-muted'
}
const countryOf = (p: OCIProfile) => countryName(p.country_code) || regionCountry(p.region)
const formatDate = (t?: string | null) => {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleDateString('zh-CN')
}
const splitTags = (tags: string) => tags.split(',').map((t) => t.trim()).filter(Boolean)
const maskOCID = (ocid?: string) => (!ocid || ocid.length < 20 ? ocid || '' : ocid.substring(0, 12) + '…' + ocid.substring(ocid.length - 8))

const onFileSelected = (event: Event) => {
  const target = event.target as HTMLInputElement
  if (target.files && target.files.length > 0) {
    const file = target.files[0]
    const reader = new FileReader()
    reader.onload = (e) => {
      importForm.value.private_key_pem = e.target?.result as string
      keyFileName.value = file.name
      message.success(`已读取私钥文件 ${file.name}`)
    }
    reader.readAsText(file)
  }
}

const submitImport = async () => {
  if (!importForm.value.raw_config.trim()) {
    message.warning('请粘贴 Oracle 控制台生成的配置块')
    return
  }
  if (!importForm.value.private_key_pem && !importForm.value.key_file_path) {
    message.warning('请提供 API 私钥：选择文件、粘贴 PEM 或填写宿主机路径')
    return
  }
  importing.value = true
  try {
    const res: any = await api.post('/profiles/import-raw', importForm.value)
    message.success(res.message || '账号已导入')
    showImportModal.value = false
    importForm.value = { raw_config: '', private_key_pem: '', key_file_path: '', tags: '', notes: '' }
    keyFileName.value = ''
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    importing.value = false
  }
}

// One account at a time, by design: never batch health checks.
const checkHealth = async (profile: OCIProfile) => {
  checkingHealthId.value = profile.id
  try {
    const res: any = await api.get(`/profiles/health/${profile.id}`)
    const result = res.result
    if (result.is_healthy) message.success(`${profile.name}：${result.message}`)
    else message.error(`${profile.name}：${result.message}`)
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    checkingHealthId.value = null
  }
}

const openEditProfileModal = (profile: OCIProfile) => {
  editingProfile.value = profile
  editForm.value = { tags: profile.tags || '', notes: profile.notes || '' }
  showEditModal.value = true
}

const submitEditProfile = async () => {
  if (!editingProfile.value) return
  savingEdit.value = true
  try {
    await api.post('/profiles/update', {
      id: editingProfile.value.id,
      tags: editForm.value.tags,
      notes: editForm.value.notes,
    })
    message.success('已保存')
    showEditModal.value = false
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    savingEdit.value = false
  }
}

const deleteProfile = (profile: OCIProfile) => {
  dialog.error({
    title: '删除账号',
    content: `确定删除账号 ${profile.name} 吗？本地保存的凭据会被清除，云上的资源不受影响。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await api.delete(`/profiles/delete/${profile.id}`)
        message.success('账号已删除')
        await profileStore.fetchProfiles()
      } catch (e: any) {
        message.error(e.message)
      }
    },
  })
}

const handleSyncLocal = async () => {
  syncing.value = true
  try {
    const res: any = await api.post('/profiles/sync-local')
    message.info(res.message)
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    syncing.value = false
  }
}

onMounted(() => {
  profileStore.fetchProfiles()
})
</script>
