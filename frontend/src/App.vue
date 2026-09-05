<template>
  <n-config-provider :theme-overrides="themeOverrides">
    <n-message-provider>
      <n-dialog-provider>
        <div class="min-h-screen flex flex-col bg-[#F9FAFB]">
          <!-- Top Navigation Bar (Hidden on login page) -->
          <header v-if="!isLoginPage" class="bg-white border-b border-gray-200 sticky top-0 z-40 shadow-sm">
            <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
              <div class="flex justify-between h-16 items-center">
                <!-- Brand & Logo -->
                <div class="flex items-center space-x-3 cursor-pointer" @click="$router.push('/')">
                  <div class="w-9 h-9 rounded-lg bg-red-600 flex items-center justify-center text-white font-bold text-lg shadow-sm">
                    ☁️
                  </div>
                  <div>
                    <span class="text-base font-bold text-gray-900 tracking-tight">OCI 免费额度控制台</span>
                    <span class="ml-2 text-xs px-2 py-0.5 rounded-full bg-red-50 text-red-600 font-semibold border border-red-200">v2.0</span>
                  </div>
                </div>

                <!-- Desktop Navigation Links -->
                <nav class="hidden md:flex space-x-1">
                  <router-link
                    v-for="item in navItems"
                    :key="item.path"
                    :to="item.path"
                    class="px-3 py-2 rounded-md text-sm font-medium transition-colors"
                    :class="$route.path === item.path ? 'bg-red-50 text-red-600 font-semibold' : 'text-gray-600 hover:text-gray-900 hover:bg-gray-100'"
                  >
                    {{ item.title }}
                  </router-link>
                </nav>

                <!-- Profile Switcher & Actions -->
                <div class="hidden md:flex items-center space-x-4">
                  <!-- Active Profile Switcher -->
                  <div class="flex items-center space-x-2 bg-gray-50 border border-gray-200 rounded-lg px-2.5 py-1">
                    <span class="text-xs text-gray-500 font-medium">当前账号:</span>
                    <n-select
                      v-model:value="profileStore.activeProfileId"
                      :options="profileOptions"
                      size="small"
                      class="w-44"
                      :loading="profileStore.loading"
                      @update:value="onProfileChange"
                    />
                  </div>

                  <!-- Panic Lockdown Button -->
                  <n-button type="error" size="small" secondary @click="handlePanicLockdown">
                    🚨 紧急锁定
                  </n-button>

                  <!-- Logout Button -->
                  <n-button size="small" quaternary @click="handleLogout">
                    退出
                  </n-button>
                </div>

                <!-- Mobile menu button -->
                <div class="flex md:hidden items-center">
                  <button
                    @click="mobileMenuOpen = !mobileMenuOpen"
                    class="p-2 rounded-md text-gray-600 hover:text-gray-900 hover:bg-gray-100"
                  >
                    <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
                    </svg>
                  </button>
                </div>
              </div>
            </div>

            <!-- Mobile menu drawer -->
            <div v-if="mobileMenuOpen" class="md:hidden border-t border-gray-200 bg-white px-4 pt-2 pb-4 space-y-2">
              <div class="mb-3">
                <span class="text-xs text-gray-500 block mb-1">切换当前账号:</span>
                <n-select
                  v-model:value="profileStore.activeProfileId"
                  :options="profileOptions"
                  size="small"
                  @update:value="onProfileChange"
                />
              </div>
              <router-link
                v-for="item in navItems"
                :key="item.path"
                :to="item.path"
                @click="mobileMenuOpen = false"
                class="block px-3 py-2 rounded-md text-base font-medium"
                :class="$route.path === item.path ? 'bg-red-50 text-red-600 font-semibold' : 'text-gray-600 hover:bg-gray-100'"
              >
                {{ item.title }}
              </router-link>
              <div class="pt-2 border-t border-gray-100 flex justify-between">
                <n-button type="error" size="small" secondary @click="handlePanicLockdown">
                  🚨 紧急锁定
                </n-button>
                <n-button size="small" quaternary @click="handleLogout">
                  退出登录
                </n-button>
              </div>
            </div>
          </header>

          <!-- Main Content View -->
          <main class="flex-1 max-w-7xl w-full mx-auto p-4 sm:p-6 lg:p-8">
            <router-view />
          </main>

          <!-- Footer -->
          <footer v-if="!isLoginPage" class="bg-white border-t border-gray-200 py-4 text-center text-xs text-gray-500">
            OCI 免费额度全自动化开机与安全控制台 © 2026 · 100% 零扣费硬防护体系 · 军工级防黑防扫
          </footer>
        </div>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { GlobalThemeOverrides } from 'naive-ui'

const route = useRoute()
const router = useRouter()
const profileStore = useProfileStore()
const mobileMenuOpen = ref(false)

const isLoginPage = computed(() => route.path === '/login')

const navItems = [
  { title: '📊 额度仪表盘', path: '/' },
  { title: '🚀 抢机开机', path: '/launcher' },
  { title: '💻 实例管理', path: '/instances' },
  { title: '💾 存储管理', path: '/storage' },
  { title: '🛡️ 防火墙', path: '/firewall' },
  { title: '👥 多账号画像', path: '/profiles' },
  { title: '⚙️ 系统设置', path: '/settings' },
]

const profileOptions = computed(() => {
  return profileStore.profiles.map(p => ({
    label: `${p.name} (${p.region})`,
    value: p.id,
  }))
})

const onProfileChange = (val: number) => {
  profileStore.setActiveProfile(val)
}

const handleLogout = async () => {
  try {
    await api.post('/auth/logout')
  } catch (e) {}
  router.push('/login')
}

const handlePanicLockdown = async () => {
  if (confirm('【高危确认】确定要触发紧急全站锁定 (Panic Lockdown) 吗？所有运行中的抢机任务将全部停机，所有会话将被强制退出！')) {
    try {
      await api.post('/auth/panic-lockdown')
      alert('已触发紧急全站锁定！会话已安全销毁。')
      router.push('/login')
    } catch (e: any) {
      alert(e.message)
    }
  }
}

onMounted(async () => {
  if (!isLoginPage.value) {
    await profileStore.fetchProfiles()
  }
})

// Naive UI clean light theme with OCI red
const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#C74634',
    primaryColorHover: '#DC2626',
    primaryColorPressed: '#991B1B',
    primaryColorSuppl: '#C74634',
    borderRadius: '8px',
  },
}
</script>
