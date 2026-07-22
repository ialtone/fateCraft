import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { useAuthStore } from './stores/auth'
import './style.css'

const app = createApp(App)
const pinia = createPinia()
const auth = useAuthStore(pinia)

router.beforeEach(async (to) => {
  await auth.initialize()
  if (to.meta.public) {
    if (to.name === 'login' && auth.authenticated) {
      const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : '/'
      return redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/'
    }
    return true
  }
  if (!auth.authenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.meta.admin && !auth.isAdmin) {
    return { name: 'conversations' }
  }
  return true
})

app.use(pinia).use(router).mount('#app')
