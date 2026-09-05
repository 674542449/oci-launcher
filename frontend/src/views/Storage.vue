<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h2 class="text-xl font-bold text-gray-900">存储全生命周期管理</h2>
        <p class="text-xs text-gray-500">引导卷在线扩容 · 磁盘性能调速 (10~120 VPU) · 磁盘快照备份 · 块存储 · 对象存储 (20GB)</p>
      </div>
      <div class="flex items-center space-x-3">
        <n-button :loading="loading" type="primary" secondary @click="fetchStorageData">
          🔄 刷新存储资源
        </n-button>
      </div>
    </div>

    <!-- Storage Tabs -->
    <n-tabs type="segment" animated>
      <!-- Tab 1: Boot Volumes -->
      <n-tab-pane name="boot" tab="💽 引导卷管理 (Boot Volumes)">
        <div class="bg-white rounded-2xl border border-gray-200 shadow-sm overflow-hidden p-6 space-y-4">
          <div class="text-xs text-gray-500 flex justify-between items-center">
            <span>列表展示当前账号下的所有引导卷，支持免关机在线扩容及自定义 VPU 性能档位 (10~120 VPU)。</span>
          </div>

          <div v-if="bootVolumes.length === 0" class="py-12 text-center text-gray-400">
            暂无活跃引导卷
          </div>
          <div v-else class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 text-left text-xs">
              <thead class="bg-gray-50 text-gray-500 font-medium">
                <tr>
                  <th class="px-4 py-3">引导卷名称</th>
                  <th class="px-4 py-3">容量大小</th>
                  <th class="px-4 py-3">磁盘性能 (VPU)</th>
                  <th class="px-4 py-3">可用区 / 状态</th>
                  <th class="px-4 py-3 text-right">操作</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 text-gray-700">
                <tr v-for="bv in bootVolumes" :key="bv.ocid">
                  <td class="px-4 py-3 font-medium text-gray-900">
                    <div>{{ bv.display_name }}</div>
                    <div class="text-[10px] font-mono text-gray-400">{{ bv.ocid.substring(0, 16) }}...</div>
                  </td>
                  <td class="px-4 py-3 font-bold text-gray-900">{{ bv.size_in_gbs }} GB</td>
                  <td class="px-4 py-3">
                    <span class="px-2 py-0.5 rounded text-xs font-semibold" :class="bv.vpus_per_gb >= 120 ? 'bg-red-50 text-red-700 border border-red-200' : 'bg-blue-50 text-blue-700'">
                      {{ bv.vpus_per_gb }} VPU
                    </span>
                  </td>
                  <td class="px-4 py-3">
                    <div>{{ bv.ad }}</div>
                    <div class="text-[10px] text-emerald-600 font-semibold">{{ bv.state }}</div>
                  </td>
                  <td class="px-4 py-3 text-right space-x-2">
                    <n-button size="tiny" secondary @click="openResizeBVModal(bv)">
                      扩容/调速
                    </n-button>
                    <n-button size="tiny" secondary @click="copyText(bv.grow_commands)">
                      扩容命令
                    </n-button>
                    <n-button size="tiny" type="info" secondary @click="openBackupModal(bv)">
                      快照备份
                    </n-button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </n-tab-pane>

      <!-- Tab 2: Block Volumes -->
      <n-tab-pane name="block" tab="🧱 块存储卷 (Block Volumes)">
        <div class="bg-white rounded-2xl border border-gray-200 shadow-sm p-6 space-y-4">
          <div class="flex justify-between items-center">
            <span class="text-xs text-gray-500">块存储与引导卷合计免费额度上限为 200 GB。</span>
            <n-button type="primary" size="small" @click="showCreateBlockModal = true">
              + 新建块存储卷
            </n-button>
          </div>

          <div v-if="blockVolumes.length === 0" class="py-12 text-center text-gray-400">
            当前暂无独立块存储卷
          </div>
          <div v-else class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 text-left text-xs">
              <thead class="bg-gray-50 text-gray-500 font-medium">
                <tr>
                  <th class="px-4 py-3">卷名称</th>
                  <th class="px-4 py-3">容量</th>
                  <th class="px-4 py-3">性能档位</th>
                  <th class="px-4 py-3">所在可用区</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 text-gray-700">
                <tr v-for="vol in blockVolumes" :key="vol.ocid">
                  <td class="px-4 py-3 font-medium text-gray-900">{{ vol.display_name }}</td>
                  <td class="px-4 py-3 font-bold">{{ vol.size_in_gbs }} GB</td>
                  <td class="px-4 py-3">{{ vol.vpus_per_gb }} VPU</td>
                  <td class="px-4 py-3">{{ vol.ad }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </n-tab-pane>

      <!-- Tab 3: Object Storage -->
      <n-tab-pane name="object" tab="📦 对象存储 (20GB 免费)">
        <div class="bg-white rounded-2xl border border-gray-200 shadow-sm p-6 space-y-4">
          <div class="flex justify-between items-center">
            <span class="text-xs text-gray-500">Always Free 免费包含 20 GB 对象存储与每月 50,000 次请求。</span>
            <n-button type="primary" size="small" @click="showCreateBucketModal = true">
              + 创建存储桶 (Bucket)
            </n-button>
          </div>

          <div v-if="buckets.length === 0" class="py-12 text-center text-gray-400">
            暂无存储桶
          </div>
          <div v-else class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
            <div v-for="b in buckets" :key="b.name" class="p-4 rounded-xl border border-gray-200 bg-gray-50/50 space-y-2">
              <div class="flex justify-between items-start">
                <span class="font-bold text-gray-900 text-sm truncate">{{ b.name }}</span>
                <button @click="deleteBucket(b.name)" class="text-red-500 hover:text-red-700 text-xs">删除</button>
              </div>
              <div class="text-[11px] text-gray-500 font-mono">命名空间: {{ b.namespace }}</div>
              <div class="text-[10px] text-gray-400">创建时间: {{ b.time_created }}</div>
            </div>
          </div>
        </div>
      </n-tab-pane>
    </n-tabs>

    <!-- Resize BV Modal -->
    <n-modal v-model:show="showResizeBVModal" preset="card" title="引导卷在线扩容与性能调速" style="max-width: 450px;">
      <div v-if="selectedBV" class="space-y-4">
        <p class="text-xs text-gray-500">调整引导卷 {{ selectedBV.display_name }} 的容量或性能（实时生效无需关机）：</p>
        <n-form-item label="目标容量大小 (GB, 必须大于当前容量，最大受 200GB 免费限额约束)">
          <n-input-number v-model:value="bvResizeForm.size" :min="selectedBV.size_in_gbs" :max="200" class="w-full" />
        </n-form-item>
        <div>
          <span class="text-xs font-medium block mb-1">性能档位: {{ bvResizeForm.vpu }} VPU (最低 10 VPU，最高 120 VPU 超高性能)</span>
          <n-slider v-model:value="bvResizeForm.vpu" :min="10" :max="120" :step="10" />
        </div>
        <div class="flex justify-end space-x-2 pt-2">
          <n-button @click="showResizeBVModal = false">取消</n-button>
          <n-button type="primary" :loading="resizingBV" @click="submitResizeBV">确认提交</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Create Block Volume Modal -->
    <n-modal v-model:show="showCreateBlockModal" preset="card" title="新建块存储卷" style="max-width: 450px;">
      <div class="space-y-4">
        <n-form-item label="卷名称">
          <n-input v-model:value="newBlockForm.name" placeholder="如 my-data-volume" />
        </n-form-item>
        <n-form-item label="容量大小 (GB)">
          <n-input-number v-model:value="newBlockForm.size" :min="50" :max="200" class="w-full" />
        </n-form-item>
        <div>
          <span class="text-xs font-medium block mb-1">性能档位: {{ newBlockForm.vpu }} VPU</span>
          <n-slider v-model:value="newBlockForm.vpu" :min="0" :max="120" :step="10" />
        </div>
        <div class="flex justify-end space-x-2 pt-2">
          <n-button @click="showCreateBlockModal = false">取消</n-button>
          <n-button type="primary" :loading="creatingBlock" @click="submitCreateBlock">创建块存储</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Create Bucket Modal -->
    <n-modal v-model:show="showCreateBucketModal" preset="card" title="创建对象存储桶" style="max-width: 450px;">
      <div class="space-y-4">
        <n-form-item label="存储桶名称 (Bucket Name)">
          <n-input v-model:value="newBucketName" placeholder="如 my-static-assets" />
        </n-form-item>
        <div class="flex justify-end space-x-2 pt-2">
          <n-button @click="showCreateBucketModal = false">取消</n-button>
          <n-button type="primary" @click="submitCreateBucket">创建存储桶</n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { useMessage } from 'naive-ui'

const profileStore = useProfileStore()
const message = useMessage()

const loading = ref(false)
const bootVolumes = ref<any[]>([])
const blockVolumes = ref<any[]>([])
const buckets = ref<any[]>([])

const showResizeBVModal = ref(false)
const selectedBV = ref<any>(null)
const bvResizeForm = ref({ size: 50, vpu: 120 })
const resizingBV = ref(false)

const showCreateBlockModal = ref(false)
const newBlockForm = ref({ name: 'oci-block-vol', size: 50, vpu: 120 })
const creatingBlock = ref(false)

const showCreateBucketModal = ref(false)
const newBucketName = ref('')

const currentProfile = computed(() => {
  return profileStore.profiles.find(p => p.id === profileStore.activeProfileId)
})

const copyText = (txt: string) => {
  navigator.clipboard.writeText(txt)
  message.success('扩容命令已复制到剪贴板！SSH 登入机器后粘贴执行即可扩展磁盘')
}

const fetchStorageData = async () => {
  if (!profileStore.activeProfileId) return
  loading.value = true
  try {
    const bvRes: any = await api.get(`/storage/boot-volumes?profile_id=${profileStore.activeProfileId}`)
    bootVolumes.value = bvRes.boot_volumes || []

    const blkRes: any = await api.get(`/storage/block-volumes?profile_id=${profileStore.activeProfileId}`)
    blockVolumes.value = blkRes.block_volumes || []

    const bktRes: any = await api.get(`/storage/buckets?profile_id=${profileStore.activeProfileId}`)
    buckets.value = bktRes.buckets || []
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loading.value = false
  }
}

const openResizeBVModal = (bv: any) => {
  selectedBV.value = bv
  bvResizeForm.value.size = bv.size_in_gbs
  bvResizeForm.value.vpu = bv.vpus_per_gb || 120
  showResizeBVModal.value = true
}

const submitResizeBV = async () => {
  if (!selectedBV.value) return
  resizingBV.value = true
  try {
    const res: any = await api.post('/storage/boot-volumes/resize', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: selectedBV.value.ocid,
      new_size_gb: bvResizeForm.value.size,
      new_vpu: bvResizeForm.value.vpu,
    })
    message.success(res.message)
    showResizeBVModal.value = false
    await fetchStorageData()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    resizingBV.value = false
  }
}

const openBackupModal = async (bv: any) => {
  const name = prompt('请输入备份快照名称:', `${bv.display_name}-backup-${Date.now()}`)
  if (!name) return
  try {
    await api.post('/storage/boot-volumes/backup', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: bv.ocid,
      name: name,
    })
    message.success('快照备份已开始创建')
  } catch (e: any) {
    message.error(e.message)
  }
}

const submitCreateBlock = async () => {
  creatingBlock.value = true
  try {
    await api.post('/storage/block-volumes/create', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ad: bootVolumes.value[0]?.ad || '',
      name: newBlockForm.value.name,
      size_gb: newBlockForm.value.size,
      vpu: newBlockForm.value.vpu,
    })
    message.success('块存储卷创建成功')
    showCreateBlockModal.value = false
    await fetchStorageData()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    creatingBlock.value = false
  }
}

const submitCreateBucket = async () => {
  if (!newBucketName.value) return
  try {
    await api.post('/storage/buckets/create', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      bucket_name: newBucketName.value,
    })
    message.success('存储桶创建成功')
    showCreateBucketModal.value = false
    await fetchStorageData()
  } catch (e: any) {
    message.error(e.message)
  }
}

const deleteBucket = async (bucketName: string) => {
  if (!confirm(`确定要删除存储桶 ${bucketName} 吗？`)) return
  try {
    await api.post('/storage/buckets/delete', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      bucket_name: bucketName,
    })
    message.success('存储桶已删除')
    await fetchStorageData()
  } catch (e: any) {
    message.error(e.message)
  }
}

watch(() => profileStore.activeProfileId, () => {
  fetchStorageData()
})

onMounted(() => {
  fetchStorageData()
})
</script>
