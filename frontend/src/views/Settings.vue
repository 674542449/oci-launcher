<template>
  <div class="mx-auto max-w-4xl">
    <PageHeader title="设置" description="Telegram 通知、管理员密码与安全审计日志。" />

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
import { NForm, NFormItem, NInput, NButton, NIcon, useMessage } from 'naive-ui'
import { SendOutline, RefreshOutline } from '@vicons/ionicons5'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

const message = useMessage()
const router = useRouter()

const tgForm = ref({ token: '', chatId: '' })
const savingTG = ref(false)
const testingTG = ref(false)

const pwdForm = ref({ oldPassword: '', newPassword: '' })
const changingPwd = ref(false)

const auditLogs = ref<any[]>([])
const loadingLogs = ref(false)

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
  loadSettings()
  fetchAuditLogs()
})
</script>
