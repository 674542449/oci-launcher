import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('./views/Login.vue'),
      meta: { title: '登录' },
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
      meta: { title: '创建实例' },
    },
    {
      path: '/instances',
      name: 'Instances',
      component: () => import('./views/Instances.vue'),
      meta: { title: '实例' },
    },
    {
      path: '/storage',
      name: 'Storage',
      component: () => import('./views/Storage.vue'),
      meta: { title: '存储' },
    },
    {
      path: '/firewall',
      name: 'Firewall',
      component: () => import('./views/Firewall.vue'),
      meta: { title: '防火墙' },
    },
    {
      path: '/profiles',
      name: 'Profiles',
      component: () => import('./views/Profiles.vue'),
      meta: { title: '账号' },
    },
    {
      path: '/billing',
      name: 'Billing',
      component: () => import('./views/Billing.vue'),
      meta: { title: '账单' },
    },
    {
      path: '/settings',
      name: 'Settings',
      component: () => import('./views/Settings.vue'),
      meta: { title: '设置' },
    },
  ],
})

router.beforeEach((to, _, next) => {
  document.title = (to.meta.title ? to.meta.title + ' · ' : '') + 'OCI 控制台'
  next()
})

export default router
