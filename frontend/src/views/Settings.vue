<template>
  <div>
    <PageHeader title="设置" description="Telegram 通知、管理员密码、IP 封禁与安全审计日志。" />

    <div class="space-y-4">
      <!-- Telegram -->
      <section class="card card-pad">
        <div class="card-head mb-5">
          <div>
            <h2 class="section-title">Telegram 通知</h2>
            <p class="caption">开机成功、任务熔断、超额告警会推送到这个聊天。</p>
          </div>
        </div>
        <n-form label-placement="top" :show-feedback="false" @submit.prevent="saveTelegramSettings">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <n-form-item label="Bot Token">
              <n-input
                v-model:value="tgForm.token"
                placeholder="从 @BotFather 获取，如 123456:ABC-DEF…"
                class="mono"
                :input-props="{ autocomplete: 'off', spellcheck: 'false' }"
              />
            </n-form-item>
            <n-form-item label="Chat ID">
              <n-input
                v-model:value="tgForm.chatId"
                placeholder="从 @userinfobot 获取的数字 ID"
                class="mono"
                :input-props="{ autocomplete: 'off', inputmode: 'numeric' }"
              />
            </n-form-item>
          </div>
          <p class="caption mt-1">已保存的 Token 会以掩码显示；重新粘贴完整 Token 即可覆盖。</p>
          <div class="mt-4 flex flex-wrap justify-end gap-2">
            <n-button secondary :loading="testingTG" @click="testTelegram">
              <template #icon><n-icon><SendOutline /></n-icon></template>
              发送测试消息
            </n-button>
            <n-button type="primary" attr-type="submit" :loading="savingTG">保存</n-button>
          </div>
        </n-form>
      </section>

      <!-- Password -->
      <section class="card card-pad">
        <div class="card-head mb-5">
          <div>
            <h2 class="section-title">修改管理员密码</h2>
            <p class="caption">修改后所有已登录会话会被注销，需要重新登录。</p>
          </div>
        </div>
        <n-form label-placement="top" :show-feedback="false" @submit.prevent="handleChangePassword">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <n-form-item label="当前密码">
              <n-input
                v-model:value="pwdForm.oldPassword"
                type="password"
                show-password-on="click"
                placeholder="当前密码"
                :input-props="{ autocomplete: 'current-password' }"
              />
            </n-form-item>
            <n-form-item label="新密码">
              <n-input
                v-model:value="pwdForm.newPassword"
                type="password"
                show-password-on="click"
                placeholder="至少 8 位"
                :input-props="{ autocomplete: 'new-password' }"
              />
            </n-form-item>
          </div>
          <div class="mt-4 flex justify-end">
            <n-button type="primary" attr-type="submit" :loading="changingPwd">更新密码</n-button>
          </div>
        </n-form>
      </section>

      <!-- SSH public keys -->
      <section class="card overflow-hidden">
        <div class="card-head card-pad pb-4">
          <div>
            <h2 class="section-title">SSH 公钥</h2>
            <p class="caption">保存常用公钥，创建实例时直接选择。默认公钥按账号设置（在创建实例页选中后点"设为该账号默认"）。</p>
          </div>
          <n-button size="small" secondary @click="showAddKey = !showAddKey">
            <template #icon><n-icon><AddOutline /></n-icon></template>
            添加公钥
          </n-button>
        </div>
        <div v-if="showAddKey" class="card-pad border-t border-line pt-4">
          <n-form label-placement="top" :show-feedback="false" @submit.prevent="addSSHKey">
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-[220px_1fr]">
              <n-form-item label="名称">
                <n-input v-model:value="keyForm.name" placeholder="例如：MacBook、工作电脑" maxlength="64" />
              </n-form-item>
              <n-form-item label="公钥">
                <n-input
                  v-model:value="keyForm.public_key"
                  type="textarea"
                  class="mono"
                  :rows="2"
                  placeholder="ssh-ed25519 AAAA… 或 ssh-rsa AAAA…"
                  :input-props="{ spellcheck: 'false' }"
                />
              </n-form-item>
            </div>
            <div class="mt-3 flex flex-wrap items-center justify-end gap-2">
              <n-button size="small" secondary @click="settingsKeyFileInput?.click()">从文件导入</n-button>
              <input ref="settingsKeyFileInput" type="file" accept=".pub,.txt,text/plain" class="sr-only" @change="onSettingsKeyFile" />
              <n-button size="small" @click="showAddKey = false">取消</n-button>
              <n-button size="small" type="primary" attr-type="submit" :loading="savingKey">保存公钥</n-button>
            </div>
          </n-form>
        </div>
        <EmptyState v-if="!loadingKeys && sshKeys.length === 0" title="还没有保存的公钥" description="添加后，创建实例时可以直接选择，不用再粘贴。" />
        <div v-else class="tbl-wrap border-t border-line">
          <table class="tbl">
            <thead>
              <tr>
                <th>名称</th>
                <th>类型</th>
                <th>指纹</th>
                <th>默认账号</th>
                <th>添加时间</th>
                <th class="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="k in sshKeys" :key="k.id">
                <td><span class="font-medium text-ink">{{ k.name }}</span></td>
                <td class="mono text-xs text-ink-2">{{ k.key_type }}</td>
                <td class="mono max-w-[320px] truncate text-xs text-ink-2" :title="k.fingerprint">{{ k.fingerprint }}</td>
                <td class="max-w-[220px] truncate text-xs text-ink-2" :title="(k.default_for || []).join('、')">{{ (k.default_for || []).length ? k.default_for.join('、') : '—' }}</td>
                <td class="mono text-xs text-ink-3">{{ formatTime(k.created_at) }}</td>
                <td class="text-right">
                  <div class="inline-flex items-center gap-1.5">
                    <n-button size="small" secondary @click="copyKey(k)">复制</n-button>
                    <n-button size="small" secondary type="error" :loading="deletingKey === k.id" @click="removeKey(k)">删除</n-button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Bans -->
      <section class="card overflow-hidden">
        <div class="card-head card-pad pb-4">
          <div>
            <h2 class="section-title">IP 封禁</h2>
            <p class="caption">密码连错 6 次封 30 分钟、12 次封 24 小时；验证码连错 15 次封 1 小时；触发蜜罐或扫描器特征封 24 小时。误封可在此解除。</p>
          </div>
          <n-button size="small" secondary :loading="loadingBans" @click="fetchBans">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
            刷新
          </n-button>
        </div>
        <EmptyState v-if="!loadingBans && bans.length === 0" title="当前没有被封禁的 IP" :description="yourIP ? `你当前的访问 IP：${yourIP}` : ''" />
        <div v-else class="tbl-wrap border-t border-line">
          <table class="tbl">
            <thead>
              <tr>
                <th>IP</th>
                <th>原因</th>
                <th>剩余时间</th>
                <th class="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="b in bans" :key="b.ip">
                <td class="mono text-[13px] font-medium text-ink">{{ b.ip }}<span v-if="b.ip === yourIP" class="pill pill-warn ml-2">当前 IP</span></td>
                <td class="text-ink-2">{{ b.reason || '—' }}</td>
                <td class="mono whitespace-nowrap text-ink-2">{{ formatRemaining(b.expires_in_secs) }}</td>
                <td class="text-right whitespace-nowrap">
                  <n-button size="small" secondary type="success" :loading="unbanning === b.ip" @click="unban(b.ip)">解封</n-button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Audit log -->
      <section class="card overflow-hidden">
        <div class="card-head card-pad pb-4">
          <div>
            <h2 class="section-title">安全审计日志</h2>
            <p class="caption">只追加、不可修改。记录登录、配置变更、开机与删机等敏感操作，最近 200 条。</p>
          </div>
          <n-button size="small" secondary :loading="loadingLogs" @click="fetchAuditLogs">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
            刷新
          </n-button>
        </div>

        <EmptyState v-if="!loadingLogs && auditLogs.length === 0" title="还没有审计记录" />
        <div v-else class="tbl-wrap max-h-[480px] overflow-y-auto border-t border-line">
          <table class="tbl">
            <thead class="sticky top-0 z-10">
              <tr>
                <th>时间</th>
                <th>操作</th>
                <th>操作者</th>
                <th>客户端 IP</th>
                <th>详情</th>
                <th>结果</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="log in auditLogs" :key="log.id">
                <td class="mono whitespace-nowrap text-ink-3">{{ formatTime(log.created_at) }}</td>
                <td><code class="mono rounded bg-surface-2 px-1.5 py-0.5 text-xs text-ink">{{ log.action }}</code></td>
                <td class="whitespace-nowrap">{{ log.operator }}</td>
                <td class="mono whitespace-nowrap text-ink-2">{{ log.client_ip }}</td>
                <td class="max-w-[360px] truncate text-ink-2" :title="log.details">{{ log.details }}</td>
                <td>
                  <span class="pill" :class="log.status === 'SUCCESS' ? 'pill-ok' : 'pill-danger'">{{ log.status === 'SUCCESS' ? '成功' : log.status }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NForm, NFormItem, NInput, NButton, NIcon, useMessage, useDialog } from 'naive-ui'
import { SendOutline, RefreshOutline, AddOutline } from '@vicons/ionicons5'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

const message = useMessage()
const router = useRouter()
const dialog = useDialog()

const tgForm = ref({ token: '', chatId: '' })
const savingTG = ref(false)
const testingTG = ref(false)

const pwdForm = ref({ oldPassword: '', newPassword: '' })
const changingPwd = ref(false)

const auditLogs = ref<any[]>([])
const loadingLogs = ref(false)

const bans = ref<any[]>([])
const loadingBans = ref(false)
const unbanning = ref('')
const yourIP = ref('')

// ---------- saved SSH public keys ----------
const sshKeys = ref<any[]>([])
const loadingKeys = ref(false)
const showAddKey = ref(false)
const keyForm = ref({ name: '', public_key: '' })
const savingKey = ref(false)
const deletingKey = ref(0)
const settingsKeyFileInput = ref<HTMLInputElement | null>(null)

const fetchSSHKeys = async () => {
  loadingKeys.value = true
  try {
    const res: any = await api.get('/ssh-keys')
    sshKeys.value = res.keys || []
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loadingKeys.value = false
  }
}

const addSSHKey = async () => {
  if (!keyForm.value.name.trim() || !keyForm.value.public_key.trim()) {
    message.warning('请填写名称并粘贴公钥')
    return
  }
  savingKey.value = true
  try {
    await api.post('/ssh-keys', { name: keyForm.value.name.trim(), public_key: keyForm.value.public_key.trim() })
    message.success('公钥已保存')
    keyForm.value = { name: '', public_key: '' }
    showAddKey.value = false
    await fetchSSHKeys()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    savingKey.value = false
  }
}

const onSettingsKeyFile = (event: Event) => {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = (e) => {
    const text = String(e.target?.result || '').trim()
    if (!/^(ssh-(rsa|ed25519|dss)|ecdsa-sha2-nistp\d+|sk-)/.test(text)) {
      message.warning('这个文件看起来不是 SSH 公钥（应以 ssh-ed25519 或 ssh-rsa 开头）。请选择 .pub 文件，而不是私钥。')
    } else {
      keyForm.value.public_key = text
      if (!keyForm.value.name.trim()) keyForm.value.name = file.name.replace(/\.pub$/i, '')
    }
    target.value = ''
  }
  reader.readAsText(file)
}

const removeKey = (k: any) => {
  dialog.warning({
    title: '删除公钥',
    content: `删除「${k.name}」后，创建实例时将无法再选择它；已经创建的实例不受影响。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      deletingKey.value = k.id
      try {
        await api.delete(`/ssh-keys/${k.id}`)
        message.success('公钥已删除')
        await fetchSSHKeys()
      } catch (e: any) {
        message.error(e.message)
      } finally {
        deletingKey.value = 0
      }
    },
  })
}

const copyKey = async (k: any) => {
  try {
    await navigator.clipboard.writeText(k.public_key)
    message.success('公钥已复制')
  } catch {
    message.error('复制失败，请手动选择复制')
  }
}

const formatRemaining = (secs: number) => {
  if (!secs || secs < 0) return '永久'
  if (secs < 60) return `${secs} 秒`
  if (secs < 3600) return `${Math.ceil(secs / 60)} 分钟`
  return `${Math.floor(secs / 3600)} 小时 ${Math.ceil((secs % 3600) / 60)} 分钟`
}

const fetchBans = async () => {
  loadingBans.value = true
  try {
    const res: any = await api.get('/security/bans')
    bans.value = res.bans || []
    yourIP.value = res.your_ip || ''
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loadingBans.value = false
  }
}

const unban = async (ip: string) => {
  unbanning.value = ip
  try {
    const res: any = await api.post('/security/unban', { ip })
    message.success(res.message || '已解封')
    await fetchBans()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    unbanning.value = ''
  }
}

const formatTime = (t: string) => {
  if (!t) return ''
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}

const loadSettings = async () => {
  try {
    const res: any = await api.get('/settings')
    if (res.settings) {
      tgForm.value.token = res.settings.tg_bot_token || ''
      tgForm.value.chatId = res.settings.tg_chat_id || ''
    }
  } catch (e) {
    /* settings are optional */
  }
}

// A masked token (from GET /settings) must not be written back over the real one.
const isMaskedToken = (t: string) => t.includes('********')

const persistTelegram = async () => {
  if (tgForm.value.token && !isMaskedToken(tgForm.value.token)) {
    await api.post('/settings/save', { key: 'tg_bot_token', value: tgForm.value.token })
  }
  if (tgForm.value.chatId) {
    await api.post('/settings/save', { key: 'tg_chat_id', value: tgForm.value.chatId })
  }
}

const saveTelegramSettings = async () => {
  if (!tgForm.value.token || !tgForm.value.chatId) {
    message.warning('请填写 Bot Token 和 Chat ID')
    return
  }
  savingTG.value = true
  try {
    await persistTelegram()
    message.success('Telegram 配置已保存')
    await loadSettings()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    savingTG.value = false
  }
}

const testTelegram = async () => {
  if (!tgForm.value.token || !tgForm.value.chatId) {
    message.warning('请先填写 Bot Token 和 Chat ID')
    return
  }
  testingTG.value = true
  try {
    await persistTelegram()
    // Sent by the backend: the browser CSP blocks direct calls to api.telegram.org.
    const res: any = await api.post('/settings/test-telegram')
    message.success(res.message || '测试消息已发送，请查看 Telegram')
  } catch (e: any) {
    message.error('发送失败：' + e.message)
  } finally {
    testingTG.value = false
  }
}

const handleChangePassword = async () => {
  if (!pwdForm.value.oldPassword || !pwdForm.value.newPassword) {
    message.warning('请填写当前密码与新密码')
    return
  }
  if (pwdForm.value.newPassword.length < 8) {
    message.warning('新密码至少 8 位')
    return
  }
  changingPwd.value = true
  try {
    await api.post('/auth/change-password', {
      old_password: pwdForm.value.oldPassword,
      new_password: pwdForm.value.newPassword,
    })
    message.success('密码已更新，请重新登录')
    setTimeout(() => router.push('/login'), 1200)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    changingPwd.value = false
  }
}

const fetchAuditLogs = async () => {
  loadingLogs.value = true
  try {
    const res: any = await api.get('/audit-logs')
    auditLogs.value = res.logs || []
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loadingLogs.value = false
  }
}

onMounted(() => {
  fetchSSHKeys()
  loadSettings()
  fetchAuditLogs()
  fetchBans()
})
</script>
