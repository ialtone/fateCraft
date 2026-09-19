import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import http from './api'
import { useAuthStore } from './stores/auth'
import './style.css'

const app = createApp(App)
const pinia = createPinia()
const auth = useAuthStore(pinia)

// 会话过期全局兜底：业务请求返回 401 时清空登录态并带回登录页（登录后
// 经 redirect 参数回到原页面）。/auth/* 自身除外——启动探测（me）与登录
// 失败本就以 401 表达业务结果。拦截器注册在这里而非 api/index.ts，
// 避免 api 模块与 auth store 相互导入成环。
http.interceptors.response.use(
  (res) => res,
  (err) => {
    const status = err.response?.status
    const url: string = err.config?.url || ''
    if (status === 401 && !url.startsWith('/auth/') && !url.startsWith('/public/')) {
      auth.expireSession()
      const current = router.currentRoute.value
      if (current.name !== 'login') {
        router.push({ name: 'login', query: { redirect: current.fullPath } })
      }
    }
    return Promise.reject(err)
  },
)

router.beforeEach(async (to) => {
  if (to.meta.public) {
    if (to.name !== 'login') return true
    await auth.initialize()
    if (to.name === 'login' && auth.authenticated) {
      const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : '/'
      return redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/'
    }
    return true
  }
  await auth.initialize()
  if (!auth.authenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.meta.admin && !auth.isAdmin) {
    return { name: 'conversations' }
  }
  return true
})

app.use(pinia).use(router).mount('#app')
