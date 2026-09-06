<template>
  <!-- Login: bare page, no chrome -->
  <router-view v-if="isLoginPage" />

  <div v-else class="min-h-screen bg-ground">
    <!-- ===== Sidebar (desktop) ===== -->
    <aside class="hidden lg:flex fixed inset-y-0 left-0 w-60 flex-col bg-side text-side-ink">
      <SideNav @navigate="() => {}" />
    </aside>

    <!-- ===== Top bar (mobile / tablet) ===== -->
    <header class="lg:hidden sticky top-0 z-30 h-14 bg-side text-side-ink flex items-center justify-between px-4">
      <button type="button" class="flex items-center gap-2.5 rounded-md" @click="router.push('/')">
        <BrandMark />
        <span class="text-[15px] font-semibold tracking-tight">OCI 控制台</span>
      </button>
      <button
        type="button"
        class="inline-flex h-10 w-10 items-center justify-center rounded-md text-side-muted hover:text-side-ink hover:bg-side-2 transition-colors"
        aria-label="打开菜单"
        @click="drawerOpen = true"
      >
        <n-icon size="22"><MenuOutline /></n-icon>
      </button>
    </header>

    <n-drawer v-model:show="drawerOpen" placement="left" :width="272" :trap-focus="true" content-style="background: var(--c-side)">
      <n-drawer-content body-content-style="padding:0" body-style="background: var(--c-side)" :native-scrollbar="false">
        <div class="min-h-full bg-side text-side-ink flex flex-col">
          <SideNav @navigate="drawerOpen = false" />
        </div>
      </n-drawer-content>
    </n-drawer>

    <!-- ===== Content ===== -->
    <div class="lg:pl-60">
      <main class="mx-auto w-full max-w-[1280px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <router-view v-slot="{ Component }">
          <transition name="page" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch, h, defineComponent } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NIcon,
  NDrawer,
  NDrawerContent,
  NSelect,
  NConfigProvider,
  darkTheme,
  useDialog,
  useMessage,
} from 'naive-ui'
import type { GlobalThemeOverrides } from 'naive-ui'
import {
  MenuOutline,
  SpeedometerOutline,
  RocketOutline,
  ServerOutline,
  LayersOutline,
  ShieldCheckmarkOutline,
  PeopleOutline,
  WalletOutline,
  SettingsOutline,
  LockClosedOutline,
  LogOutOutline,
} from '@vicons/ionicons5'
import { useProfileStore } from '@/stores/profile'
import { api } from '@/api/client'
import { regionLabel, regionFlag } from '@/lib/regions'

const route = useRoute()
const router = useRouter()
const profileStore = useProfileStore()
const dialog = useDialog()
const message = useMessage()

const drawerOpen = ref(false)
const isLoginPage = computed(() => route.path === '/login')

const navItems = [
  { title: '额度仪表盘', path: '/', icon: SpeedometerOutline },
  { title: '创建实例', path: '/launcher', icon: RocketOutline },
  { title: '实例', path: '/instances', icon: ServerOutline },
  { title: '存储', path: '/storage', icon: LayersOutline },
  { title: '防火墙', path: '/firewall', icon: ShieldCheckmarkOutline },
  { title: '账号', path: '/profiles', icon: PeopleOutline },
  { title: '账单', path: '/billing', icon: WalletOutline },
  { title: '设置', path: '/settings', icon: SettingsOutline },
]

const profileOptions = computed(() =>
  profileStore.profiles.map((p) => ({ label: `${regionFlag(p.region)} ${p.name} · ${regionLabel(p.region)}`.trim(), value: p.id })),
)
const activeProfile = computed(() => profileStore.profiles.find((p) => p.id === profileStore.activeProfileId))

const onProfileChange = (val: number) => profileStore.setActiveProfile(val)

const handleLogout = async () => {
  try {
    await api.post('/auth/logout')
  } catch (e) {
    /* session may already be gone */
  }
  localStorage.removeItem('token')
  router.push('/login')
}

const handlePanicLockdown = () => {
  dialog.error({
    title: '触发全站紧急锁定？',
    content: '所有排队中的创建任务将立即停止，所有登录会话将被强制注销。此操作用于疑似凭据泄露等紧急情况。',
    positiveText: '确认锁定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await api.post('/auth/panic-lockdown')
        message.success('已触发紧急锁定，会话已注销')
        router.push('/login')
      } catch (e: any) {
        message.error(e.message)
      }
    },
  })
}

// Dark rail: reuse Naive's dark theme so the account selector matches the sidebar.
const darkOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#e0604c',
    primaryColorHover: '#e8735f',
    primaryColorPressed: '#c74634',
    primaryColorSuppl: '#e0604c',
    borderRadius: '8px',
    inputColor: '#292524',
    popoverColor: '#292524',
    borderColor: '#3b3733',
    hoverColor: '#3b3733',
    textColor2: '#e7e5e4',
    textColor3: '#a8a29e',
    fontFamily: 'var(--font-sans)',
  },
}

const BrandMark = defineComponent({
  name: 'BrandMark',
  setup() {
    return () =>
      h(
        'span',
        {
          class:
            'inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-brand text-white shadow-[inset_0_-1px_0_rgba(0,0,0,.25)]',
          'aria-hidden': 'true',
        },
        [
          h(
            'svg',
            { viewBox: '0 0 32 32', width: 20, height: 20, fill: 'none', stroke: 'currentColor', 'stroke-width': 2, 'stroke-linejoin': 'round' },
            [h('path', { d: 'M10.5 21.5a4.5 4.5 0 0 1-.6-8.96A6 6 0 0 1 21.4 11.2a4 4 0 0 1 .6 7.95V21.5h-11.5z' })],
          ),
        ],
      )
  },
})

const SideNav = defineComponent({
  name: 'SideNav',
  emits: ['navigate'],
  setup(_, { emit }) {
    const go = (path: string) => {
      emit('navigate')
      router.push(path)
    }
    return () =>
      h('div', { class: 'flex h-full flex-col' }, [
        // brand
        h('div', { class: 'flex items-center gap-3 px-5 pt-5 pb-4' }, [
          h(BrandMark),
          h('div', { class: 'min-w-0' }, [
            h('div', { class: 'text-[15px] font-semibold tracking-tight leading-5' }, 'OCI 控制台'),
            h('div', { class: 'text-xs text-side-muted leading-4' }, '免费额度 · 多账号'),
          ]),
        ]),
        // account switcher
        h('div', { class: 'px-4 pb-4' }, [
          h('div', { class: 'text-[11px] font-medium uppercase tracking-wider text-side-muted mb-1.5 px-1' }, '当前账号'),
          h(
            NConfigProvider,
            { theme: darkTheme, themeOverrides: darkOverrides, abstract: true },
            {
              default: () =>
                h(NSelect, {
                  value: profileStore.activeProfileId,
                  options: profileOptions.value,
                  loading: profileStore.loading,
                  placeholder: '尚未导入账号',
                  size: 'medium',
                  'onUpdate:value': onProfileChange,
                }),
            },
          ),
          activeProfile.value
            ? h('div', { class: 'mt-2 flex items-center gap-2 px-1 text-xs text-side-muted' }, [
                h('span', {
                  class: [
                    'h-1.5 w-1.5 rounded-full',
                    activeProfile.value.status === 'Active' ? 'bg-emerald-400' : 'bg-red-400',
                  ],
                }),
                h('span', { class: 'truncate' }, activeProfile.value.status === 'Active' ? '账号状态正常' : activeProfile.value.status_message || '账号状态异常'),
              ])
            : null,
        ]),
        // nav
        h(
          'nav',
          { class: 'flex-1 px-3 space-y-0.5', 'aria-label': '主导航' },
          navItems.map((item) => {
            const active = route.path === item.path
            return h(
              'button',
              {
                type: 'button',
                class: [
                  'relative flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-[13.5px] font-medium transition-colors',
                  active ? 'bg-side-2 text-white' : 'text-side-muted hover:text-side-ink hover:bg-side-2/60',
                ],
                'aria-current': active ? 'page' : undefined,
                onClick: () => go(item.path),
              },
              [
                active
                  ? h('span', { class: 'absolute left-0 top-2 bottom-2 w-0.5 rounded-full bg-brand', 'aria-hidden': 'true' })
                  : null,
                h(NIcon, { size: 18, class: 'shrink-0' }, { default: () => h(item.icon) }),
                h('span', null, item.title),
              ],
            )
          }),
        ),
        // footer actions
        h('div', { class: 'px-3 pb-4 pt-3 space-y-1 border-t border-side-2' }, [
          h(
            'button',
            {
              type: 'button',
              class:
                'flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-[13.5px] font-medium text-red-300 hover:text-red-200 hover:bg-side-2/60 transition-colors',
              onClick: () => {
                emit('navigate')
                handlePanicLockdown()
              },
            },
            [h(NIcon, { size: 18 }, { default: () => h(LockClosedOutline) }), h('span', null, '紧急锁定')],
          ),
          h(
            'button',
            {
              type: 'button',
              class:
                'flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-[13.5px] font-medium text-side-muted hover:text-side-ink hover:bg-side-2/60 transition-colors',
              onClick: () => {
                emit('navigate')
                handleLogout()
              },
            },
            [h(NIcon, { size: 18 }, { default: () => h(LogOutOutline) }), h('span', null, '退出登录')],
          ),
          h('div', { class: 'px-3 pt-2 text-[11px] text-side-muted/70' }, 'v2.0'),
        ]),
      ])
  },
})

onMounted(async () => {
  if (!isLoginPage.value) await profileStore.fetchProfiles()
})

// Coming back from the login page: (re)load profiles.
watch(isLoginPage, async (isLogin, wasLogin) => {
  if (!isLogin && wasLogin) await profileStore.fetchProfiles()
})

</script>
