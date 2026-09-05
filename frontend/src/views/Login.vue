<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-100 px-4">
    <div class="max-w-md w-full bg-white rounded-2xl shadow-xl border border-gray-100 p-8 space-y-6">
      <!-- Brand Header -->
      <div class="text-center space-y-2">
        <div class="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-red-600 text-white text-2xl shadow-md">
          ☁️
        </div>
        <h1 class="text-2xl font-bold text-gray-900 tracking-tight">OCI 免费额度控制台</h1>
        <p class="text-xs text-gray-500">双因素认证 (2FA) · 军工级抗爆破防护体系</p>
      </div>

      <!-- State 1: First-time Bootstrap Admin Initialization -->
      <div v-if="isUninitialized" class="space-y-4">
        <div class="p-3 bg-amber-50 border border-amber-200 rounded-lg text-xs text-amber-800">
          👋 欢迎首次使用！请初始化超级管理员账号并绑定手机双因素身份验证器 (2FA)。
        </div>

        <div v-if="!bootstrapDone" class="space-y-4">
          <n-form-item label="管理员账号">
            <n-input v-model:value="initForm.username" placeholder="请输入管理员用户名" />
          </n-form-item>
          <n-form-item label="管理员密码">
            <n-input v-model:value="initForm.password" type="password" show-password-on="click" placeholder="请输入至少 8 位强密码" />
          </n-form-item>
          <n-button type="primary" block :loading="initLoading" @click="handleInitAdmin">
            下一步：绑定 2FA 验证器
          </n-button>
        </div>

        <!-- 2FA QR Code Display -->
        <div v-else class="text-center space-y-4">
          <p class="text-xs text-gray-600">请使用 Google Authenticator、1Password 或微软验证器扫描此二维码：</p>
          <div class="flex justify-center">
            <canvas ref="qrCanvas" class="border p-2 rounded-lg bg-white"></canvas>
          </div>
          <div class="bg-gray-50 p-2 rounded text-xs font-mono select-all text-gray-700">
            密钥: {{ init2FASecret }}
          </div>
          <n-button type="primary" block @click="isUninitialized = false; step = 1">
            我已完成扫码绑定，前往登录
          </n-button>
        </div>
      </div>

      <!-- State 2: Standard 2-Step Login -->
      <div v-else class="space-y-4">
        <!-- Step 1: Username & Password -->
        <div v-if="step === 1" class="space-y-4">
          <n-form-item label="管理账号">
            <n-input v-model:value="loginForm.username" placeholder="用户名" />
          </n-form-item>
          <n-form-item label="登录密码">
            <n-input v-model:value="loginForm.password" type="password" show-password-on="click" placeholder="密码" @keyup.enter="handleStep1Login" />
          </n-form-item>
          <n-button type="primary" block :loading="loginLoading" @click="handleStep1Login">
            验证密码
          </n-button>
        </div>

        <!-- Step 2: 2FA TOTP Code -->
        <div v-else class="space-y-4">
          <div class="text-center py-2">
            <span class="text-sm font-medium text-gray-700">请输入手机 Authenticator 6位动态验证码</span>
          </div>
          <n-form-item label="2FA 动态码">
            <n-input
              v-model:value="totpCode"
              placeholder="6 位数字 (如 123456)"
              maxlength="6"
              size="large"
              class="text-center tracking-widest text-lg font-mono"
              @keyup.enter="handleStep2Verify"
            />
          </n-form-item>
          <n-button type="primary" block :loading="loginLoading" @click="handleStep2Verify">
            安全登入控制台
          </n-button>
          <n-button quaternary block size="small" @click="step = 1">
            返回重新输入密码
          </n-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useMessage } from 'naive-ui'
import QRCode from 'qrcode'

const router = useRouter()
const message = useMessage()

const isUninitialized = ref(false)
const bootstrapDone = ref(false)
const initLoading = ref(false)
const init2FASecret = ref('')
const qrCanvas = ref<HTMLCanvasElement | null>(null)

const initForm = ref({
  username: 'admin',
  password: '',
})

const step = ref(1)
const loginLoading = ref(false)
const tempToken = ref('')
const totpCode = ref('')

const loginForm = ref({
  username: '',
  password: '',
})

const checkStatus = async () => {
  try {
    const res: any = await api.get('/auth/status')
    if (!res.initialized) {
      isUninitialized.value = true
    }
  } catch (e: any) {
    message.error(e.message)
  }
}

const handleInitAdmin = async () => {
  if (!initForm.value.password || initForm.value.password.length < 8) {
    message.warning('密码长度至少为 8 位')
    return
  }
  initLoading.value = true
  try {
    const res: any = await api.post('/auth/init', initForm.value)
    init2FASecret.value = res.totp_secret
    bootstrapDone.value = true
    await nextTick()
    if (qrCanvas.value && res.totp_qr_url) {
      await QRCode.toCanvas(qrCanvas.value, res.totp_qr_url, { width: 180, margin: 1 })
    }
    message.success('管理员已初始化，请务必扫码保存 2FA 密钥！')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    initLoading.value = false
  }
}

const handleStep1Login = async () => {
  if (!loginForm.value.username || !loginForm.value.password) {
    message.warning('请输入账号与密码')
    return
  }
  loginLoading.value = true
  try {
    const res: any = await api.post('/auth/login', loginForm.value)
    if (res.require_2fa) {
      tempToken.value = res.temp_token
      step.value = 2
      message.info('密码验证通过，请输入 2FA 动态码')
    }
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loginLoading.value = false
  }
}

const handleStep2Verify = async () => {
  if (!totpCode.value || totpCode.value.length !== 6) {
    message.warning('请输入完整的 6 位数字验证码')
    return
  }
  loginLoading.value = true
  try {
    const res: any = await api.post('/auth/2fa/verify', {
      temp_token: tempToken.value,
      code: totpCode.value,
    })
    if (res.token) {
      localStorage.setItem('token', res.token)
    }
    message.success('登录成功！正在进入控制台...')
    setTimeout(() => {
      router.push('/')
    }, 500)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loginLoading.value = false
  }
}

onMounted(() => {
  checkStatus()
})
</script>
