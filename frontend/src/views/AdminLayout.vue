<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { NTag } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const auth = useAuthStore()

const navItems = [
  { to: '/admin', label: '总览', exact: true },
  { to: '/admin/users', label: '用户与权限' },
  { to: '/admin/settings', label: '全局设置' },
  { to: '/admin/auth', label: '账号与注册' },
  { to: '/admin/identities', label: '第三方账户' },
  { to: '/admin/logs', label: '诊断日志' },
  { to: '/admin/rewards', label: '奖励管理' },
  { to: '/admin/gameplay', label: '玩法能力' },
  { to: '/admin/content', label: '内置内容' },
  { to: '/admin/challenges', label: '挑战内容' },
  { to: '/admin/shares', label: '分享治理' },
]

const roleLabel = computed(() => (auth.isOwner ? '平台拥有者' : '管理员'))
</script>

<template>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="管理员导航">
      <div class="admin-sidebar-head">
        <div class="admin-kicker">FATECRAFT</div>
        <h1>管理后台</h1>
        <NTag size="small" :type="auth.isOwner ? 'warning' : 'info'">{{ roleLabel }}</NTag>
      </div>
      <nav class="admin-nav">
        <RouterLink
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="admin-nav-link"
          :class="{ active: item.exact ? route.path === item.to : route.path.startsWith(item.to) }"
        >
          {{ item.label }}
        </RouterLink>
      </nav>
      <RouterLink to="/" class="back-link">返回用户端</RouterLink>
    </aside>
    <main class="admin-content">
      <RouterView />
    </main>
  </div>
</template>

<style scoped>
.admin-shell {
  display: grid;
  grid-template-columns: 216px minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  height: 100%;
  min-height: 0;
  background: var(--bg);
}

.admin-sidebar {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  overflow-y: auto;
  padding: 28px 14px 20px;
  border-right: 1px solid var(--border-soft);
  background: color-mix(in srgb, var(--bg-soft) 70%, transparent);
}

.admin-sidebar-head {
  padding: 0 10px 24px;
}

.admin-kicker {
  color: var(--accent);
  font-size: 10px;
  letter-spacing: 2px;
}

h1 {
  margin: 7px 0 12px;
  font-size: 21px;
}

.admin-nav {
  display: grid;
  gap: 2px;
}

.admin-nav-link,
.back-link {
  display: block;
  padding: 9px 10px;
  border-left: 2px solid transparent;
  color: var(--text-dim);
  font-size: 13px;
}

.admin-nav-link:hover,
.admin-nav-link.active {
  border-left-color: var(--accent);
  color: var(--text);
  background: color-mix(in srgb, var(--accent-soft) 42%, transparent);
}

.back-link {
  margin-top: auto;
  border-top: 1px solid var(--border-soft);
  padding-top: 16px;
}

.admin-content {
  min-width: 0;
  min-height: 0;
  padding: 28px 32px 48px;
  overflow: auto;
}

@media (max-width: 760px) {
  .admin-shell {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto minmax(0, 1fr);
  }

  .admin-sidebar {
    padding: 14px 12px 0;
    border-right: 0;
    border-bottom: 1px solid var(--border-soft);
  }

  .admin-sidebar-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 0 12px;
  }

  .admin-sidebar-head h1 {
    margin: 0;
    font-size: 18px;
  }

  .admin-kicker {
    display: none;
  }

  .admin-nav {
    display: flex;
    gap: 2px;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .admin-nav::-webkit-scrollbar {
    display: none;
  }

  .admin-nav-link {
    flex: 0 0 auto;
    border-left: 0;
    border-bottom: 2px solid transparent;
    white-space: nowrap;
  }

  .admin-nav-link:hover,
  .admin-nav-link.active {
    border-bottom-color: var(--accent);
    border-left-color: transparent;
  }

  .back-link {
    display: none;
  }

  .admin-content {
    padding: 20px 12px 32px;
  }
}
</style>
