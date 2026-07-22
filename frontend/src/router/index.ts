import { createRouter, createWebHistory } from 'vue-router'

// 路由 meta 类型收窄：title 用于 afterEach 统一设置 document.title。
declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    public?: boolean
    admin?: boolean
  }
}

/** 无页面标题时的兜底站点标题（与 index.html 的 <title> 保持一致） */
const BASE_TITLE = '命运茶馆 Fatecraft'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { title: '登录', public: true },
    },
    {
      path: '/',
      name: 'conversations',
      component: () => import('@/views/ConversationListView.vue'),
    },
    {
      path: '/conversations/:id',
      name: 'conversation',
      component: () => import('@/views/ConversationView.vue'),
      meta: { title: '对话' },
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/views/SettingsView.vue'),
      meta: { title: '设置' },
    },
    {
      path: '/admin',
      name: 'admin',
      component: () => import('@/views/AdminView.vue'),
      meta: { title: '管理', admin: true },
    },
    {
      // 兜底 404：history 模式下服务器把所有路径都回退到 index.html，
      // 没有这条路由时任意乱路径会静默渲染成空白首页（软 404）。
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
      meta: { title: '页面不存在' },
    },
  ],
})

// 按路由 meta 统一维护标签页标题（首页保持站点标题）。
router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · 命运茶馆` : BASE_TITLE
})

export default router
