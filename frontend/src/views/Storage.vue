<template>
  <div>
    <PageHeader title="存储" description="引导卷扩容与性能调整、快照备份、块存储卷和对象存储桶。引导卷与块存储合计免费 200 GB。">
      <template #actions>
        <n-button secondary :loading="loading" :disabled="!currentProfile" @click="fetchStorageData">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新
        </n-button>
      </template>
    </PageHeader>

    <n-tabs v-model:value="activeTab" type="line" animated>
      <!-- ===== Boot volumes ===== -->
      <n-tab-pane name="boot" :tab="`引导卷 ${bootVolumes.length ? '· ' + bootVolumes.length : ''}`">
        <div class="card overflow-hidden">
          <div class="card-head card-pad pb-4">
            <div>
              <h2 class="section-title">引导卷</h2>
              <p class="caption">在线扩容无需关机；扩容后需在系统内执行扩容命令。性能档位 10–120 VPU。</p>
            </div>
          </div>
          <SkeletonRows v-if="loading && bootVolumes.length === 0" />
          <EmptyState v-else-if="bootVolumes.length === 0" title="没有引导卷" description="创建实例后，它的引导卷会显示在这里。" />
          <div v-else class="tbl-wrap border-t border-line">
            <table class="tbl">
              <thead>
                <tr>
                  <th>名称</th>
                  <th>容量</th>
                  <th>性能</th>
                  <th>可用区 / 状态</th>
                  <th class="text-right">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="bv in bootVolumes" :key="bv.ocid">
                  <td class="min-w-[220px]">
                    <div class="font-medium text-ink">{{ bv.display_name }}</div>
                    <div class="mono mt-0.5 text-xs text-ink-3" :title="bv.ocid">{{ maskOCID(bv.ocid) }}</div>
                  </td>
                  <td class="mono whitespace-nowrap text-[14px] font-semibold text-ink">{{ bv.size_in_gbs }} <span class="text-xs font-normal text-ink-3">GB</span></td>
                  <td><VpuBadge :vpu="bv.vpus_per_gb" /></td>
                  <td>
                    <div class="mono text-xs text-ink-2">{{ shortAD(bv.ad) }}</div>
                    <StatusPill class="mt-1" :state="bv.state" />
                  </td>
                  <td class="text-right whitespace-nowrap">
                    <div class="inline-flex items-center gap-1.5">
                      <n-button size="small" secondary @click="openResizeBVModal(bv)">扩容 / 调速</n-button>
                      <n-button size="small" secondary :disabled="!bv.grow_commands" @click="copyGrow(bv.grow_commands)">复制扩容命令</n-button>
                      <n-button size="small" secondary type="info" @click="openBackupModal(bv)">快照备份</n-button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </n-tab-pane>

      <!-- ===== Block volumes ===== -->
      <n-tab-pane name="block" :tab="`块存储卷 ${blockVolumes.length ? '· ' + blockVolumes.length : ''}`">
        <div class="card overflow-hidden">
          <div class="card-head card-pad pb-4">
            <div>
              <h2 class="section-title">块存储卷</h2>
              <p class="caption">独立于实例的数据盘，需挂载到实例后使用。</p>
            </div>
            <n-button type="primary" size="small" @click="openCreateBlock">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              新建块存储卷
            </n-button>
          </div>
          <SkeletonRows v-if="loading && blockVolumes.length === 0" />
          <EmptyState v-else-if="blockVolumes.length === 0" title="没有独立块存储卷" description="新建后可挂载到任意同可用区的实例。" />
          <div v-else class="tbl-wrap border-t border-line">
            <table class="tbl">
              <thead>
                <tr>
                  <th>名称</th>
                  <th>容量</th>
                  <th>性能</th>
                  <th>可用区 / 状态</th>
                  <th>创建时间</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="vol in blockVolumes" :key="vol.ocid">
                  <td>
                    <div class="font-medium text-ink">{{ vol.display_name }}</div>
                    <div class="mono mt-0.5 text-xs text-ink-3" :title="vol.ocid">{{ maskOCID(vol.ocid) }}</div>
                  </td>
                  <td class="mono whitespace-nowrap text-[14px] font-semibold text-ink">{{ vol.size_in_gbs }} <span class="text-xs font-normal text-ink-3">GB</span></td>
                  <td><VpuBadge :vpu="vol.vpus_per_gb" /></td>
                  <td>
                    <div class="mono text-xs text-ink-2">{{ shortAD(vol.ad) }}</div>
                    <StatusPill class="mt-1" :state="vol.state" />
                  </td>
                  <td class="mono whitespace-nowrap text-xs text-ink-3">{{ formatDate(vol.time_created) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </n-tab-pane>

      <!-- ===== Object storage ===== -->
      <n-tab-pane name="object" :tab="`对象存储 ${buckets.length ? '· ' + buckets.length : ''}`">
        <div class="card card-pad">
          <div class="card-head mb-5">
            <div>
              <h2 class="section-title">存储桶</h2>
              <p class="caption">Always Free 包含 20 GB 对象存储与每月 50,000 次请求。</p>
            </div>
            <n-button type="primary" size="small" @click="showCreateBucketModal = true">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              创建存储桶
            </n-button>
          </div>
          <EmptyState v-if="!loading && buckets.length === 0" title="没有存储桶" description="创建一个桶来存放静态文件或备份。" />
          <div v-else class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="b in buckets" :key="b.name" class="rounded-lg border border-line bg-surface-2/60 p-4 transition-colors hover:border-line-strong">
              <div class="flex items-start justify-between gap-2">
                <div class="flex min-w-0 items-center gap-2">
                  <n-icon size="18" class="shrink-0 text-ink-3"><CubeOutline /></n-icon>
                  <span class="truncate text-[14px] font-semibold text-ink" :title="b.name">{{ b.name }}</span>
                </div>
                <button type="button" class="txt-btn-muted hover:!text-danger" @click="deleteBucket(b.name)">删除</button>
              </div>
              <dl class="mono mt-3 space-y-1 text-xs text-ink-3">
                <div class="flex justify-between gap-2"><dt>命名空间</dt><dd class="truncate text-ink-2">{{ b.namespace }}</dd></div>
                <div class="flex justify-between gap-2"><dt>层级</dt><dd class="text-ink-2">{{ b.storage_tier || 'Standard' }}</dd></div>
                <div v-if="b.approx_size_gb" class="flex justify-between gap-2"><dt>约占用</dt><dd class="text-ink-2">{{ b.approx_size_gb }} GB</dd></div>
                <div class="flex justify-between gap-2"><dt>创建于</dt><dd class="text-ink-2">{{ formatDate(b.time_created) }}</dd></div>
              </dl>
            </div>
          </div>
        </div>
      </n-tab-pane>
    </n-tabs>

    <!-- Resize boot volume -->
    <n-modal v-model:show="showResizeBVModal" preset="card" title="引导卷扩容与性能调整" style="max-width: 460px" :bordered="false">
      <div v-if="selectedBV" class="space-y-5">
        <p class="caption">调整 <b class="text-ink">{{ selectedBV.display_name }}</b>。容量只能增大不能缩小，最大受 200 GB 免费额度约束。</p>
        <div>
          <span class="label">目标容量（GB）</span>
          <n-input-number v-model:value="bvResizeForm.size" :min="selectedBV.size_in_gbs" :max="200" :step="10" class="w-full" />
        </div>
        <div>
          <div class="mb-1.5 flex items-center justify-between">
            <span class="label mb-0">性能档位</span>
            <span class="mono text-sm font-semibold text-ink">{{ bvResizeForm.vpu }} VPU</span>
          </div>
          <n-slider v-model:value="bvResizeForm.vpu" :min="10" :max="120" :step="10" :marks="{ 10: '均衡', 20: '高', 120: '超高' }" />
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <n-button @click="showResizeBVModal = false">取消</n-button>
          <n-button type="primary" :loading="resizingBV" @click="submitResizeBV">提交</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Backup -->
    <n-modal v-model:show="showBackupModal" preset="card" title="创建引导卷快照" style="max-width: 460px" :bordered="false">
      <div v-if="selectedBV" class="space-y-4">
        <p class="caption">对 <b class="text-ink">{{ selectedBV.display_name }}</b> 创建一份完整备份，可用于恢复或克隆实例。</p>
        <div>
          <span class="label">快照名称</span>
          <n-input v-model:value="backupName" placeholder="备份名称" />
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <n-button @click="showBackupModal = false">取消</n-button>
          <n-button type="primary" :loading="backingUp" @click="submitBackup">开始备份</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Create block volume -->
    <n-modal v-model:show="showCreateBlockModal" preset="card" title="新建块存储卷" style="max-width: 460px" :bordered="false">
      <div class="space-y-4">
        <div>
          <span class="label">名称</span>
          <n-input v-model:value="newBlockForm.name" placeholder="如 data-vol-1" />
        </div>
        <div>
          <span class="label">可用区</span>
          <n-select v-model:value="newBlockForm.ad" :options="adOptions" placeholder="选择可用区" />
          <p v-if="adOptions.length === 0" class="caption mt-1">读不到可用区列表。请先创建一个实例，或稍后重试。</p>
        </div>
        <div>
          <span class="label">容量（GB）</span>
          <n-input-number v-model:value="newBlockForm.size" :min="50" :max="200" :step="10" class="w-full" />
        </div>
        <div>
          <div class="mb-1.5 flex items-center justify-between">
            <span class="label mb-0">性能档位</span>
            <span class="mono text-sm font-semibold text-ink">{{ newBlockForm.vpu }} VPU</span>
          </div>
          <n-slider v-model:value="newBlockForm.vpu" :min="0" :max="120" :step="10" :marks="{ 0: '低成本', 10: '均衡', 20: '高', 120: '超高' }" />
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <n-button @click="showCreateBlockModal = false">取消</n-button>
          <n-button type="primary" :loading="creatingBlock" :disabled="!newBlockForm.ad" @click="submitCreateBlock">创建</n-button>
        </div>
      </div>
    </n-modal>

    <!-- Create bucket -->
    <n-modal v-model:show="showCreateBucketModal" preset="card" title="创建存储桶" style="max-width: 460px" :bordered="false">
      <div class="space-y-4">
        <div>
          <span class="label">存储桶名称</span>
          <n-input v-model:value="newBucketName" placeholder="如 static-assets" :input-props="{ spellcheck: 'false' }" />
          <p class="caption mt-1">同一命名空间内名称必须唯一，只允许字母、数字、连字符和下划线。</p>
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <n-button @click="showCreateBucketModal = false">取消</n-button>
          <n-button type="primary" :loading="creatingBucket" @click="submitCreateBucket">创建</n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, h, defineComponent } from 'vue'
import { NButton, NIcon, NTabs, NTabPane, NModal, NInput, NInputNumber, NSlider, NSelect, NSkeleton, useMessage, useDialog } from 'naive-ui'
import { RefreshOutline, AddOutline, CubeOutline } from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import StatusPill from '@/components/StatusPill.vue'
import EmptyState from '@/components/EmptyState.vue'

const profileStore = useProfileStore()
const message = useMessage()
const dialog = useDialog()

const activeTab = ref('boot')
const loading = ref(false)
const bootVolumes = ref<any[]>([])
const blockVolumes = ref<any[]>([])
const buckets = ref<any[]>([])
const availabilityDomains = ref<string[]>([])

const showResizeBVModal = ref(false)
const selectedBV = ref<any>(null)
const bvResizeForm = ref({ size: 50, vpu: 120 })
const resizingBV = ref(false)

const showBackupModal = ref(false)
const backupName = ref('')
const backingUp = ref(false)

const showCreateBlockModal = ref(false)
const newBlockForm = ref({ name: 'data-vol-1', ad: '', size: 50, vpu: 10 })
const creatingBlock = ref(false)

const showCreateBucketModal = ref(false)
const newBucketName = ref('')
const creatingBucket = ref(false)

const currentProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))

const adOptions = computed(() => {
  const set = new Set<string>(availabilityDomains.value)
  for (const v of [...bootVolumes.value, ...blockVolumes.value]) if (v.ad) set.add(v.ad)
  return Array.from(set).map((ad) => ({ label: ad, value: ad }))
})

const maskOCID = (ocid?: string) => (!ocid || ocid.length < 20 ? ocid || '' : ocid.substring(0, 16) + '…' + ocid.substring(ocid.length - 6))
const shortAD = (ad?: string) => (ad ? ad.replace(/^[^:]+:/, '') : '')
const formatDate = (t?: string) => {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? t : d.toLocaleDateString('zh-CN')
}

const VpuBadge = defineComponent({
  props: { vpu: { type: Number, default: 0 } },
  setup(props) {
    return () => {
      const v = props.vpu
      const label = v >= 30 ? '超高性能' : v >= 20 ? '高性能' : v >= 10 ? '均衡' : '低成本'
      const cls = v >= 30 ? 'bg-brand-soft text-brand' : v >= 20 ? 'bg-info-soft text-info' : 'bg-surface-2 text-ink-2'
      return h('span', { class: ['mono inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium whitespace-nowrap', cls] }, [
        h('span', { class: 'font-semibold' }, `${v} VPU`),
        h('span', { class: 'opacity-70' }, label),
      ])
    }
  },
})

const SkeletonRows = defineComponent({
  setup() {
    return () =>
      h(
        'div',
        { class: 'divide-y divide-line border-t border-line' },
        [1, 2].map((i) =>
          h('div', { key: i, class: 'flex items-center gap-6 px-5 py-4' }, [
            h(NSkeleton, { text: true, width: '26%' }),
            h(NSkeleton, { text: true, width: '10%' }),
            h(NSkeleton, { text: true, width: '14%' }),
            h(NSkeleton, { text: true, width: '20%' }),
          ]),
        ),
      )
  },
})

const copyGrow = async (cmd: string) => {
  try {
    await navigator.clipboard.writeText(cmd)
    message.success('扩容命令已复制，SSH 登录后粘贴执行即可')
  } catch {
    message.error('复制失败，请手动复制')
  }
}

const fetchAvailabilityDomains = async () => {
  if (!profileStore.activeProfileId) return
  try {
    const res: any = await api.get(`/network/ads?profile_id=${profileStore.activeProfileId}`)
    availabilityDomains.value = (res.ads || []).map((a: any) => (typeof a === 'string' ? a : a.name))
  } catch {
    availabilityDomains.value = []
  }
}

const fetchStorageData = async () => {
  if (!profileStore.activeProfileId) return
  loading.value = true
  const pid = profileStore.activeProfileId
  const errors: string[] = []
  const [bv, blk, bkt] = await Promise.allSettled([
    api.get(`/storage/boot-volumes?profile_id=${pid}`),
    api.get(`/storage/block-volumes?profile_id=${pid}`),
    api.get(`/storage/buckets?profile_id=${pid}`),
  ])
  if (bv.status === 'fulfilled') bootVolumes.value = (bv.value as any).boot_volumes || []
  else errors.push(bv.reason?.message)
  if (blk.status === 'fulfilled') blockVolumes.value = (blk.value as any).block_volumes || []
  else errors.push(blk.reason?.message)
  if (bkt.status === 'fulfilled') buckets.value = (bkt.value as any).buckets || []
  else errors.push(bkt.reason?.message)
  loading.value = false
  if (errors.length) message.error(errors[0])
}

const openResizeBVModal = (bv: any) => {
  selectedBV.value = bv
  bvResizeForm.value.size = bv.size_in_gbs
  bvResizeForm.value.vpu = bv.vpus_per_gb || 10
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

const openBackupModal = (bv: any) => {
  selectedBV.value = bv
  const stamp = new Date().toISOString().slice(0, 10)
  backupName.value = `${bv.display_name}-backup-${stamp}`.replace(/\s+/g, '-')
  showBackupModal.value = true
}

const submitBackup = async () => {
  if (!selectedBV.value || !backupName.value.trim()) return
  backingUp.value = true
  try {
    await api.post('/storage/boot-volumes/backup', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ocid: selectedBV.value.ocid,
      name: backupName.value.trim(),
    })
    message.success('快照备份已开始创建')
    showBackupModal.value = false
  } catch (e: any) {
    message.error(e.message)
  } finally {
    backingUp.value = false
  }
}

const openCreateBlock = () => {
  if (!newBlockForm.value.ad && adOptions.value.length > 0) newBlockForm.value.ad = adOptions.value[0].value
  showCreateBlockModal.value = true
}

const submitCreateBlock = async () => {
  if (!newBlockForm.value.name.trim() || !newBlockForm.value.ad) return
  creatingBlock.value = true
  try {
    await api.post('/storage/block-volumes/create', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      ad: newBlockForm.value.ad,
      name: newBlockForm.value.name.trim(),
      size_gb: newBlockForm.value.size,
      vpu: newBlockForm.value.vpu,
    })
    message.success('块存储卷已创建')
    showCreateBlockModal.value = false
    await fetchStorageData()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    creatingBlock.value = false
  }
}

const submitCreateBucket = async () => {
  if (!newBucketName.value.trim()) return
  creatingBucket.value = true
  try {
    await api.post('/storage/buckets/create', {
      profile_id: profileStore.activeProfileId,
      region: currentProfile.value?.region,
      bucket_name: newBucketName.value.trim(),
    })
    message.success('存储桶已创建')
    showCreateBucketModal.value = false
    newBucketName.value = ''
    await fetchStorageData()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    creatingBucket.value = false
  }
}

const deleteBucket = (bucketName: string) => {
  dialog.error({
    title: '删除存储桶',
    content: `确定删除存储桶 ${bucketName} 吗？桶必须为空才能删除，删除后无法恢复。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        // The backend route is DELETE with a JSON body.
        await api.delete('/storage/buckets/delete', {
          data: {
            profile_id: profileStore.activeProfileId,
            region: currentProfile.value?.region,
            bucket_name: bucketName,
          },
        })
        message.success('存储桶已删除')
        await fetchStorageData()
      } catch (e: any) {
        message.error(e.message)
      }
    },
  })
}

watch(
  () => profileStore.activeProfileId,
  () => {
    bootVolumes.value = []
    blockVolumes.value = []
    buckets.value = []
    newBlockForm.value.ad = ''
    fetchStorageData()
    fetchAvailabilityDomains()
  },
)

onMounted(() => {
  fetchStorageData()
  fetchAvailabilityDomains()
})
</script>
