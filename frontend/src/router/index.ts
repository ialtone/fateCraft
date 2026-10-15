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
const BASE_TITLE = '命运茶馆 fateCraft'

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
      path: '/setup',
      name: 'setup',
      component: () => import('@/views/SetupView.vue'),
      meta: { title: '首次安装', public: true },
    },
    {
      path: '/s/:token',
      name: 'shared-conversation',
      component: () => import('@/views/SharedConversationView.vue'),
      meta: { title: '故事分享', public: true },
    },
    {
      path: '/shared/:token',
      name: 'shared-conversation-client',
      component: () => import('@/views/SharedConversationView.vue'),
      meta: { title: '故事分享', public: true },
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
      path: '/shares',
      name: 'shares',
      component: () => import('@/views/ConversationListView.vue'),
      meta: { title: '我的分享' },
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/views/SettingsView.vue'),
      meta: { title: '设置' },
    },
    {
      path: '/rewards',
      name: 'rewards',
      component: () => import('@/views/RewardsView.vue'),
      meta: { title: '我的奖励' },
    },
    {
      path: '/admin',
      component: () => import('@/views/AdminLayout.vue'),
      meta: { title: '管理', admin: true },
      children: [
        {
          path: '',
          name: 'admin',
          component: () => import('@/views/AdminDashboardView.vue'),
          meta: { title: '管理总览' },
        },
        {
          path: 'users',
          name: 'admin-users',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'users' },
          meta: { title: '用户与权限' },
        },
        {
          path: 'settings',
          name: 'admin-settings',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'settings' },
          meta: { title: '全局设置' },
        },
        {
          path: 'auth',
          name: 'admin-auth',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'auth' },
          meta: { title: '账号与注册' },
        },
        {
          path: 'identities',
          name: 'admin-identities',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'identities' },
          meta: { title: '第三方账户' },
        },
        {
          path: 'logs',
          name: 'admin-logs',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'logs' },
          meta: { title: '诊断日志' },
        },
        {
          path: 'rewards',
          name: 'admin-rewards',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'rewards' },
          meta: { title: '奖励管理' },
        },
        {
          path: 'gameplay',
          name: 'admin-gameplay',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'gameplay' },
          meta: { title: '玩法能力' },
        },
        {
          path: 'content',
          name: 'admin-content',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'content' },
          meta: { title: '内置内容' },
        },
        {
          path: 'challenges',
          name: 'admin-challenges',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'challenges' },
          meta: { title: '挑战内容' },
        },
        {
          path: 'shares',
          name: 'admin-shares',
          component: () => import('@/views/AdminView.vue'),
          props: { section: 'sharing' },
          meta: { title: '分享治理' },
        },
      ],
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
