import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('./views/Login.vue'),
      meta: { title: '安全登录与 2FA 验证' },
    },
    {
      path: '/',
      name: 'Dashboard',
      component: () => import('./views/Dashboard.vue'),
      meta: { title: '额度仪表盘' },
    },
    {
      path: '/launcher',
      name: 'Launcher',
      component: () => import('./views/Launcher.vue'),
      meta: { title: '抢机开机控制台' },
    },
    {
      path: '/instances',
      name: 'Instances',
      component: () => import('./views/Instances.vue'),
      meta: { title: '实例与网络管理' },
    },
    {
      path: '/storage',
      name: 'Storage',
      component: () => import('./views/Storage.vue'),
      meta: { title: '存储全生命周期管理' },
    },
    {
      path: '/firewall',
      name: 'Firewall',
      component: () => import('./views/Firewall.vue'),
      meta: { title: '防火墙与安全列表' },
    },
    {
      path: '/profiles',
      name: 'Profiles',
      component: () => import('./views/Profiles.vue'),
      meta: { title: '多 Profile 账号画像' },
    },
    {
      path: '/settings',
      name: 'Settings',
      component: () => import('./views/Settings.vue'),
      meta: { title: '系统设置与审计' },
    },
  ],
})

router.beforeEach((to, _, next) => {
  document.title = (to.meta.title ? to.meta.title + ' - ' : '') + 'OCI 免费额度控制台'
  next()
})

export default router
