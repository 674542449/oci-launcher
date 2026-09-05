<template>
  <div class="min-h-screen bg-ground flex flex-col">
    <div class="flex flex-1 items-center justify-center px-4 py-10">
      <div class="w-full max-w-[400px]">
        <!-- Brand -->
        <div class="mb-6 flex items-center gap-3">
          <span class="inline-flex h-10 w-10 items-center justify-center rounded-xl bg-brand text-white" aria-hidden="true">
            <svg viewBox="0 0 32 32" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round">
              <path d="M10.5 21.5a4.5 4.5 0 0 1-.6-8.96A6 6 0 0 1 21.4 11.2a4 4 0 0 1 .6 7.95V21.5h-11.5z" />
            </svg>
          </span>
          <div>
            <h1 class="text-lg font-semibold tracking-tight text-ink leading-6">OCI 控制台</h1>
            <p class="text-xs text-ink-3">Oracle Cloud 免费额度 · 多账号运维</p>
          </div>
        </div>

        <div class="card p-6 sm:p-7">
          <!-- ===== First run: create the admin and bind 2FA ===== -->
          <template v-if="isUninitialized">
            <div v-if="!bootstrapDone" class="space-y-5">
              <div>
                <h2 class="text-[15px] font-semibold text-ink">初始化管理员</h2>
                <p class="caption mt-1">首次使用，请创建管理员账号。下一步将绑定手机验证器（TOTP）。</p>
              </div>
              <n-form label-placement="top" :show-feedback="false" class="space-y-4" @submit.prevent="handleInitAdmin">
                <n-form-item label="管理员账号">
                  <n-input v-model:value="initForm.username" placeholder="admin" :input-props="{ autocomplete: 'username' }" />
                </n-form-item>
                <n-form-item label="管理员密码">
                  <n-input
                    v-model:value="initForm.password"
                    type="password"
                    show-password-on="click"
                    placeholder="至少 8 位"
                    :input-props="{ autocomplete: 'new-password' }"
                  />
                </n-form-item>
                <n-button type="primary" block attr-type="submit" :loading="initLoading" size="large">
                  创建并绑定验证器
                </n-button>
              </n-form>
            </div>

            <div v-else class="space-y-5">
              <div>
                <h2 class="text-[15px] font-semibold text-ink">绑定验证器</h2>
                <p class="caption mt-1">用 Google Authenticator、1Password 或 Microsoft Authenticator 扫描二维码。</p>
              </div>
              <div class="flex justify-center">
                <canvas ref="qrCanvas" class="rounded-lg border border-line bg-white p-2" aria-label="2FA 二维码"></canvas>
              </div>
              <div class="rounded-lg border border-line bg-surface-2 px-3 py-2">
                <div class="caption mb-0.5">无法扫码时手动输入密钥</div>
                <code class="mono block break-all text-[13px] text-ink select-all">{{ init2FASecret }}</code>
              </div>
              <div class="notice notice-warn">
                <n-icon size="18" class="mt-0.5 shrink-0"><WarningOutline /></n-icon>
                <span>密钥只显示这一次。丢失后需要重置数据库才能重新绑定。</span>
              </div>
              <n-button type="primary" block size="large" @click="isUninitialized = false; step = 1">
                已完成绑定，去登录
              </n-button>
            </div>
          </template>

          <!-- ===== Normal login: password, then one-time code ===== -->
          <template v-else>
            <ol class="mb-5 flex items-center gap-2 text-xs" aria-label="登录步骤">
              <li class="flex items-center gap-1.5" :class="step === 1 ? 'text-ink font-medium' : 'text-ink-3'">
                <span class="mono inline-flex h-5 w-5 items-center justify-center rounded-full border text-[11px]" :class="step === 1 ? 'border-ink text-ink' : 'border-line text-ink-3'">1</span>
                密码
              </li>
              <li class="h-px w-6 bg-line" aria-hidden="true"></li>
              <li class="flex items-center gap-1.5" :class="step === 2 ? 'text-ink font-medium' : 'text-ink-3'">
                <span class="mono inline-flex h-5 w-5 items-center justify-center rounded-full border text-[11px]" :class="step === 2 ? 'border-ink text-ink' : 'border-line text-ink-3'">2</span>
                动态码
              </li>
            </ol>

            <n-form v-if="step === 1" label-placement="top" :show-feedback="false" class="space-y-4" @submit.prevent="handleStep1Login">
              <n-form-item label="账号">
                <n-input v-model:value="loginForm.username" placeholder="用户名" :input-props="{ autocomplete: 'username' }" />
              </n-form-item>
              <n-form-item label="密码">
                <n-input
                  v-model:value="loginForm.password"
                  type="password"
                  show-password-on="click"
                  placeholder="密码"
                  :input-props="{ autocomplete: 'current-password' }"
                />
              </n-form-item>
              <n-button type="primary" block attr-type="submit" :loading="loginLoading" size="large">继续</n-button>
            </n-form>

            <n-form v-else label-placement="top" :show-feedback="false" class="space-y-4" @submit.prevent="handleStep2Verify">
              <div>
                <h2 class="text-[15px] font-semibold text-ink">输入 6 位动态码</h2>
                <p class="caption mt-1">打开手机验证器，输入当前显示的验证码。</p>
              </div>
              <n-form-item label="动态码">
                <n-input
                  ref="totpInput"
                  v-model:value="totpCode"
                  placeholder="123456"
                  maxlength="6"
                  size="large"
                  class="mono text-center text-lg tracking-[0.35em]"
                  :input-props="{ inputmode: 'numeric', autocomplete: 'one-time-code', pattern: '[0-9]*' }"
                  :allow-input="onlyDigits"
                />
              </n-form-item>
              <n-button type="primary" block attr-type="submit" :loading="loginLoading" size="large">登录</n-button>
              <n-button quaternary block size="small" @click="step = 1">返回重新输入密码</n-button>
            </n-form>
          </template>
        </div>

        <p class="mt-5 text-center text-xs text-ink-3">登录受速率限制与两步验证保护。</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { NForm, NFormItem, NInput, NButton, NIcon, useMessage } from 'naive-ui'
import { WarningOutline } from '@vicons/ionicons5'
import { api } from '@/api/client'
import QRCode from 'qrcode'

const router = useRouter()
const message = useMessage()

const isUninitialized = ref(false)
const bootstrapDone = ref(false)
const initLoading = ref(false)
const init2FASecret = ref('')
const qrCanvas = ref<HTMLCanvasElement | null>(null)
const totpInput = ref<any>(null)

const initForm = ref({ username: 'admin', password: '' })

const step = ref(1)
const loginLoading = ref(false)
const tempToken = ref('')
const totpCode = ref('')
const loginForm = ref({ username: '', password: '' })

const onlyDigits = (v: string) => !v || /^\d+$/.test(v)

const checkStatus = async () => {
  try {
    const res: any = await api.get('/auth/status')
    if (!res.initialized) isUninitialized.value = true
  } catch (e: any) {
    message.error(e.message)
  }
}

const handleInitAdmin = async () => {
  if (!initForm.value.username.trim()) {
    message.warning('请输入管理员账号')
    return
  }
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
      await QRCode.toCanvas(qrCanvas.value, res.totp_qr_url, { width: 184, margin: 1 })
    }
    message.success('管理员已创建，请扫码绑定验证器')
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
      await nextTick()
      totpInput.value?.focus?.()
    }
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loginLoading.value = false
  }
}

const handleStep2Verify = async () => {
  if (!totpCode.value || totpCode.value.length !== 6) {
    message.warning('请输入完整的 6 位验证码')
    return
  }
  loginLoading.value = true
  try {
    const res: any = await api.post('/auth/2fa/verify', {
      temp_token: tempToken.value,
      code: totpCode.value,
    })
    if (res.token) localStorage.setItem('token', res.token)
    message.success('登录成功')
    router.push('/')
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
