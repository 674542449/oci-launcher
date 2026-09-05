<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h2 class="text-xl font-bold text-gray-900">多 Profile 账号画像管理</h2>
        <p class="text-xs text-gray-500">原格式 API 一键粘贴导入 · 私钥安全加密 · 多彩标签画像 · 严格单号独立健康体检</p>
      </div>

      <div class="flex items-center space-x-3">
        <n-button type="primary" @click="showImportModal = true">
          + 导入 / 添加 OCI 账号
        </n-button>
        <n-button secondary @click="handleSyncLocal">
          📂 同步宿主机配置
        </n-button>
      </div>
    </div>

    <!-- Search & Filter Bar -->
    <div class="bg-white p-4 rounded-xl border border-gray-200 shadow-sm flex items-center space-x-3">
      <span class="text-xs text-gray-500 font-medium">按别名或标签搜索:</span>
      <n-input v-model:value="searchKeyword" placeholder="输入关键字过滤账号..." size="small" class="max-w-xs" />
    </div>

    <!-- Profiles List Cards -->
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      <div
        v-for="p in filteredProfiles"
        :key="p.id"
        class="bg-white p-6 rounded-2xl border transition-all space-y-4 shadow-sm"
        :class="p.id === profileStore.activeProfileId ? 'border-red-500 ring-2 ring-red-100' : 'border-gray-200 hover:border-gray-300'"
      >
        <!-- Top row: Name, Region, Status -->
        <div class="flex justify-between items-start">
          <div>
            <div class="flex items-center space-x-2">
              <span class="text-base font-bold text-gray-900">{{ p.name }}</span>
              <span v-if="p.id === profileStore.activeProfileId" class="text-[10px] px-2 py-0.5 rounded-full bg-red-50 text-red-600 font-bold border border-red-200">
                当前活跃
              </span>
            </div>
            <div class="text-xs text-gray-500 font-mono">{{ p.region }}</div>
          </div>

          <!-- Status badge -->
          <span
            class="px-2.5 py-0.5 rounded-full text-xs font-semibold"
            :class="p.status === 'Active' ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : 'bg-red-50 text-red-700 border border-red-200 animate-pulse'"
          >
            {{ p.status === 'Active' ? '活跃正常' : '异常/封号' }}
          </span>
        </div>

        <!-- Tags List -->
        <div v-if="p.tags" class="flex flex-wrap gap-1.5">
          <span
            v-for="t in p.tags.split(',')"
            :key="t"
            class="px-2 py-0.5 rounded-md text-[11px] bg-gray-100 text-gray-700 font-medium"
          >
            🏷️ {{ t.trim() }}
          </span>
        </div>

        <!-- Notes / Metadata -->
        <div v-if="p.notes" class="text-xs text-gray-500 bg-gray-50 p-2.5 rounded-lg border border-gray-100">
          📝 备注: {{ p.notes }}
        </div>

        <!-- OCID details -->
        <div class="text-[11px] font-mono text-gray-400 space-y-0.5 border-t border-gray-100 pt-3">
          <div class="truncate" :title="p.tenancy_ocid">租户: {{ maskOCID(p.tenancy_ocid) }}</div>
          <div class="truncate">指纹: {{ p.fingerprint }}</div>
        </div>

        <!-- Card Footer Actions -->
        <div class="flex items-center justify-between pt-2 border-t border-gray-100 text-xs">
          <!-- Switch Active Button -->
          <n-button
            v-if="p.id !== profileStore.activeProfileId"
            size="tiny"
            type="primary"
            secondary
            @click="profileStore.setActiveProfile(p.id)"
          >
            设为当前账号
          </n-button>
          <span v-else class="text-xs font-semibold text-red-600">正在操作此账号</span>

          <div class="space-x-1.5">
            <!-- Single-Account Health Check -->
            <n-button size="tiny" secondary :loading="checkingHealthId === p.id" @click="checkHealth(p)">
              体检
            </n-button>
            <n-button size="tiny" secondary @click="openEditProfileModal(p)">
              编辑
            </n-button>
            <n-button size="tiny" type="error" secondary @click="deleteProfile(p)">
              删除
            </n-button>
          </div>
        </div>
      </div>
    </div>

    <!-- Import Profile Modal (Raw INI + Key Upload) -->
    <n-modal v-model:show="showImportModal" preset="card" title="导入 OCI 账号凭据 (原格式直接粘贴)" style="max-width: 600px;">
      <div class="space-y-4">
        <div class="p-3 bg-blue-50 border border-blue-200 rounded-lg text-xs text-blue-800">
          💡 从 Oracle 控制台「添加 API 密钥」后，直接整段复制官方给出的配置代码块粘贴至下方，系统自动解析全部参数：
        </div>

        <n-form-item label="Oracle 原格式配置块内容">
          <n-input
            v-model:value="importForm.raw_config"
            type="textarea"
            placeholder="[DEFAULT]&#10;user=ocid1.user.oc1..aaaaaaa...&#10;fingerprint=12:34:56:...&#10;tenancy=ocid1.tenancy.oc1..aaaaaaa...&#10;region=us-ashburn-1"
            rows="5"
            class="font-mono text-xs"
          />
        </n-form-item>

        <!-- Private Key Input Options -->
        <div class="space-y-2 bg-gray-50 p-4 rounded-xl border border-gray-200">
          <span class="text-xs font-bold text-gray-800">API 私钥提供途径 (三选一)</span>
          <n-tabs type="line" size="small">
            <!-- Option A: File upload -->
            <n-tab-pane name="file" tab="📂 本地选择 .pem 文件">
              <input type="file" accept=".pem,.key" @change="onFileSelected" class="text-xs text-gray-600 file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border-0 file:text-xs file:font-semibold file:bg-red-50 file:text-red-700 hover:file:bg-red-100" />
            </n-tab-pane>
            <!-- Option B: Manual text paste -->
            <n-tab-pane name="text" tab="✍️ 手动粘贴 PEM 明文">
              <n-input
                v-model:value="importForm.private_key_pem"
                type="textarea"
                placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;...&#10;-----END RSA PRIVATE KEY-----"
                rows="4"
                class="font-mono text-xs"
              />
            </n-tab-pane>
            <!-- Option C: Server path -->
            <n-tab-pane name="path" tab="🖥️ 宿主机文件路径">
              <n-input v-model:value="importForm.key_file_path" placeholder="如 ~/.oci/oci_api_key.pem" size="small" />
            </n-tab-pane>
          </n-tabs>
        </div>

        <!-- Tags & Notes -->
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <n-form-item label="自定义标签 (逗号分隔)">
            <n-input v-model:value="importForm.tags" placeholder="如 主力号,已升级,首尔区" />
          </n-form-item>
          <n-form-item label="备注说明 (注册邮箱/卡号后四位)">
            <n-input v-model:value="importForm.notes" placeholder="如 user@gmail.com, 招行卡" />
          </n-form-item>
        </div>

        <div class="flex justify-end space-x-2 pt-2 border-t border-gray-100">
          <n-button @click="showImportModal = false">取消</n-button>
          <n-button type="primary" :loading="importing" @click="submitImport">确认导入并验证连通性</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Edit Profile Modal -->
    <n-modal v-model:show="showEditModal" preset="card" title="编辑账号画像与标签" style="max-width: 450px;">
      <div v-if="editingProfile" class="space-y-4">
        <n-form-item label="账号多彩标签 (逗号分隔)">
          <n-input v-model:value="editForm.tags" placeholder="如 主力号,已升级,东京A1" />
        </n-form-item>
        <n-form-item label="私有备注说明">
          <n-input v-model:value="editForm.notes" type="textarea" placeholder="填写备忘信息..." rows="3" />
        </n-form-item>
        <div class="flex justify-end space-x-2 pt-2">
          <n-button @click="showEditModal = false">取消</n-button>
          <n-button type="primary" @click="submitEditProfile">保存修改</n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useProfileStore, OCIProfile } from '@/stores/profile'
import { api } from '@/api/client'
import { useMessage } from 'naive-ui'

const profileStore = useProfileStore()
const message = useMessage()

const searchKeyword = ref('')
const showImportModal = ref(false)
const importing = ref(false)
const checkingHealthId = ref<number | null>(null)

const showEditModal = ref(false)
const editingProfile = ref<OCIProfile | null>(null)
const editForm = ref({ tags: '', notes: '' })

const importForm = ref({
  raw_config: '',
  private_key_pem: '',
  key_file_path: '',
  tags: '',
  notes: '',
})

const filteredProfiles = computed(() => {
  if (!searchKeyword.value) return profileStore.profiles
  const kw = searchKeyword.value.toLowerCase()
  return profileStore.profiles.filter(p =>
    p.name.toLowerCase().includes(kw) ||
    p.region.toLowerCase().includes(kw) ||
    (p.tags && p.tags.toLowerCase().includes(kw)) ||
    (p.notes && p.notes.toLowerCase().includes(kw))
  )
})

const maskOCID = (ocid?: string) => {
  if (!ocid || ocid.length < 20) return ocid || ''
  return ocid.substring(0, 12) + '...' + ocid.substring(ocid.length - 8)
}

const onFileSelected = (event: Event) => {
  const target = event.target as HTMLInputElement
  if (target.files && target.files.length > 0) {
    const file = target.files[0]
    const reader = new FileReader()
    reader.onload = (e) => {
      importForm.value.private_key_pem = e.target?.result as string
      message.success(`已加载私钥文件: ${file.name}`)
    }
    reader.readAsText(file)
  }
}

const submitImport = async () => {
  if (!importForm.value.raw_config) {
    message.warning('请粘贴 Oracle 控制台的配置文本')
    return
  }
  importing.value = true
  try {
    const res: any = await api.post('/profiles/import-raw', importForm.value)
    message.success(res.message)
    showImportModal.value = false
    importForm.value = { raw_config: '', private_key_pem: '', key_file_path: '', tags: '', notes: '' }
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    importing.value = false
  }
}

// Single-Account Health Check
// STRICT: Only single check, no batch!
const checkHealth = async (profile: OCIProfile) => {
  checkingHealthId.value = profile.id
  try {
    const res: any = await api.get(`/profiles/health/${profile.id}`)
    const result = res.result
    if (result.is_healthy) {
      message.success(`账号 [${profile.name}] 体检正常: ${result.message}`)
    } else {
      message.error(`账号 [${profile.name}] 体检异常: ${result.message}`)
    }
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
  try {
    await api.post('/profiles/update', {
      id: editingProfile.value.id,
      tags: editForm.value.tags,
      notes: editForm.value.notes,
    })
    message.success('画像标签已更新')
    showEditModal.value = false
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  }
}

const deleteProfile = async (profile: OCIProfile) => {
  if (!confirm(`确定要删除 OCI 账号 [${profile.name}] 吗？`)) return
  try {
    await api.delete(`/profiles/delete/${profile.id}`)
    message.success('账号已删除')
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  }
}

const handleSyncLocal = async () => {
  try {
    const res: any = await api.post('/profiles/sync-local')
    message.info(res.message)
    await profileStore.fetchProfiles()
  } catch (e: any) {
    message.error(e.message)
  }
}

onMounted(() => {
  profileStore.fetchProfiles()
})
</script>
