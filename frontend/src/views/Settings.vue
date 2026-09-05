<template>
  <div class="space-y-6 max-w-4xl mx-auto">
    <!-- Header -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm">
      <h2 class="text-xl font-bold text-gray-900">系统设置与安全审计</h2>
      <p class="text-xs text-gray-500">Telegram Bot 消息推送 · 密码与 2FA 修改 · 不可篡改安全审计日志</p>
    </div>

    <!-- 1. Telegram Bot Configuration -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
      <div class="flex justify-between items-center border-b border-gray-100 pb-3">
        <div>
          <h3 class="text-sm font-bold text-gray-900">Telegram Bot 消息通知配置</h3>
          <p class="text-xs text-gray-500">开机成功、异常熔断、超额警报即时富文本推送到您的 Telegram</p>
        </div>
        <n-button size="small" type="primary" secondary :loading="testingTG" @click="testTelegram">
          📲 发送测试消息
        </n-button>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
        <n-form-item label="Telegram Bot Token">
          <n-input v-model:value="tgForm.token" placeholder="从 @BotFather 获取的 Token (如 123456:ABC-DEF...)" />
        </n-form-item>
        <n-form-item label="Telegram Chat ID">
          <n-input v-model:value="tgForm.chatId" placeholder="从 @userinfobot 获取的数字 ID (如 123456789)" />
        </n-form-item>
      </div>

      <div class="flex justify-end">
        <n-button type="primary" :loading="savingTG" @click="saveTelegramSettings">
          保存 Telegram 配置
        </n-button>
      </div>
    </div>

    <!-- 2. Change Admin Password -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
      <h3 class="text-sm font-bold text-gray-900 border-b border-gray-100 pb-3">修改管理员密码 (Argon2id / Bcrypt 强哈希)</h3>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
        <n-form-item label="当前旧密码">
          <n-input v-model:value="pwdForm.oldPassword" type="password" show-password-on="click" placeholder="旧密码" />
        </n-form-item>
        <n-form-item label="设置新密码">
          <n-input v-model:value="pwdForm.newPassword" type="password" show-password-on="click" placeholder="新密码 (至少8位)" />
        </n-form-item>
      </div>

      <div class="flex justify-end">
        <n-button type="primary" :loading="changingPwd" @click="handleChangePassword">
          更新密码并吊销旧会话
        </n-button>
      </div>
    </div>

    <!-- 3. Immutable Security Audit Logs -->
    <div class="bg-white p-6 rounded-2xl border border-gray-200 shadow-sm space-y-4">
      <div class="flex justify-between items-center border-b border-gray-100 pb-3">
        <div>
          <h3 class="text-sm font-bold text-gray-900">不可篡改安全审计日志 (Append-Only)</h3>
          <p class="text-xs text-gray-500">记录全站敏感操作流水（登录、改配、开机、删机、安全拦截）</p>
        </div>
        <n-button size="small" @click="fetchAuditLogs">刷新审计日志</n-button>
      </div>

      <div class="overflow-x-auto max-h-96">
        <table class="min-w-full divide-y divide-gray-200 text-left text-xs">
          <thead class="bg-gray-50 text-gray-500 sticky top-0">
            <tr>
              <th class="px-4 py-2.5">时间</th>
              <th class="px-4 py-2.5">操作行为</th>
              <th class="px-4 py-2.5">操作人</th>
              <th class="px-4 py-2.5">客户端 IP</th>
              <th class="px-4 py-2.5">详细说明</th>
              <th class="px-4 py-2.5">状态</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 text-gray-700 font-mono">
            <tr v-for="log in auditLogs" :key="log.id" class="hover:bg-gray-50">
              <td class="px-4 py-2 text-gray-400 whitespace-nowrap">{{ formatTime(log.created_at) }}</td>
              <td class="px-4 py-2 font-bold">{{ log.action }}</td>
              <td class="px-4 py-2">{{ log.operator }}</td>
              <td class="px-4 py-2">{{ log.client_ip }}</td>
              <td class="px-4 py-2 font-sans max-w-xs truncate" :title="log.details">{{ log.details }}</td>
              <td class="px-4 py-2">
                <span class="px-2 py-0.5 rounded text-[10px]" :class="log.status === 'SUCCESS' ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-red-700'">
                  {{ log.status }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/api/client'
import { useMessage } from 'naive-ui'
import { useRouter } from 'vue-router'

const message = useMessage()
const router = useRouter()

const tgForm = ref({ token: '', chatId: '' })
const savingTG = ref(false)
const testingTG = ref(false)

const pwdForm = ref({ oldPassword: '', newPassword: '' })
const changingPwd = ref(false)

const auditLogs = ref<any[]>([])

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
  } catch (e) {}
}

const saveTelegramSettings = async () => {
  savingTG.value = true
  try {
    await api.post('/settings/save', { key: 'tg_bot_token', value: tgForm.value.token })
    await api.post('/settings/save', { key: 'tg_chat_id', value: tgForm.value.chatId })
    message.success('Telegram Bot 配置已安全保存')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    savingTG.value = false
  }
}

const testTelegram = async () => {
  if (!tgForm.value.token || !tgForm.value.chatId) {
    message.warning('请先填写完整的 Bot Token 和 Chat ID')
    return
  }
  testingTG.value = true
  try {
    await api.post('/settings/save', { key: 'tg_bot_token', value: tgForm.value.token })
    await api.post('/settings/save', { key: 'tg_chat_id', value: tgForm.value.chatId })
    // Directly trigger test via telegram send API
    const url = `https://api.telegram.org/bot${tgForm.value.token}/sendMessage`
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        chat_id: tgForm.value.chatId,
        text: '🔔 <b>【OCI 控制台测试推送】</b>\n这是一条连通性测试消息，您的 Telegram 通知配置完全正常！',
        parse_mode: 'HTML',
      }),
    })
    if (resp.ok) {
      message.success('测试推送成功！请查看您的 Telegram 聊天窗口')
    } else {
      message.error(`Telegram API 报错，状态码: ${resp.status}`)
    }
  } catch (e: any) {
    message.error('发送测试消息失败: ' + e.message)
  } finally {
    testingTG.value = false
  }
}

const handleChangePassword = async () => {
  if (!pwdForm.value.oldPassword || !pwdForm.value.newPassword) {
    message.warning('请填写旧密码与新密码')
    return
  }
  changingPwd.value = true
  try {
    await api.post('/auth/change-password', {
      old_password: pwdForm.value.oldPassword,
      new_password: pwdForm.value.newPassword,
    })
    message.success('密码已成功修改！旧会话已被吊销，请重新登录')
    setTimeout(() => {
      router.push('/login')
    }, 1500)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    changingPwd.value = false
  }
}

const fetchAuditLogs = async () => {
  try {
    const res: any = await api.get('/audit-logs')
    auditLogs.value = res.logs || []
  } catch (e) {}
}

onMounted(() => {
  loadSettings()
  fetchAuditLogs()
})
</script>
