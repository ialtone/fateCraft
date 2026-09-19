<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { GiftOutline, LogOutOutline } from '@vicons/ionicons5'
import {
  NAvatar,
  NButton,
  NConfigProvider,
  NDialogProvider,
  NIcon,
  NMessageProvider,
  NTooltip,
} from 'naive-ui'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { useThemeStore } from '@/stores/theme'
import { useRewardStore } from '@/stores/rewards'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const theme = useThemeStore()
const rewards = useRewardStore()
const auth = useAuthStore()

// 主对话页使用全屏三栏布局，不显示顶部导航。
const isConversation = computed(() => route.name === 'conversation')
const isAuthPage = computed(() => route.name === 'login')
const isSharedPage = computed(
  () => route.name === 'shared-conversation' || route.name === 'shared-conversation-client',
)

const navs = computed(() => [
  {
    to: '/',
    label: '我的对话',
    active: route.name === 'conversations' || route.name === 'shares',
  },
  { to: '/rewards', label: '奖励' },
  { to: '/settings', label: '设置' },
  ...(auth.isAdmin ? [{ to: '/admin', label: '管理' }] : []),
])

// 加载账号数据
onMounted(() => {
  watch(
    () => auth.user,
    (user) => {
      if (user) rewards.load().catch(() => {})
      else rewards.reset()
    },
    { immediate: true },
  )
})

async function logout() {
  await auth.logout()
  await router.replace('/login')
}
</script>

<template>
  <NConfigProvider
    :theme="theme.naiveTheme"
    :theme-overrides="theme.overrides"
    style="height: 100%"
  >
    <NMessageProvider placement="top">
      <NDialogProvider>
        <div class="app">
          <header v-if="!isConversation && !isAuthPage && !isSharedPage" class="topnav">
            <RouterLink to="/" class="brand">
              <span class="lantern" aria-hidden="true"></span>
              <span class="brand-cn">命运茶馆</span>
              <span class="brand-en">Fatecraft</span>
            </RouterLink>
            <nav class="links">
              <RouterLink
                v-for="n in navs"
                :key="n.to"
                :to="n.to"
                class="nav-link"
                :class="{ 'section-active': n.active }"
              >
                {{ n.label }}
              </RouterLink>
            </nav>
            <div class="nav-right">
              <RouterLink
                v-if="rewards.loaded"
                to="/rewards"
                class="account-brief"
                aria-label="查看我的奖励"
              >
                <span
                  v-for="item in rewards.featured.slice(0, 2)"
                  :key="item.definition.key"
                  class="stat-item"
                  :title="item.definition.description || item.definition.name"
                >
                  <NIcon class="stat-icon" :component="GiftOutline" />
                  <span class="stat-name">{{ item.definition.name }}</span>
                  <span class="stat-value">{{ item.balance.toLocaleString() }}</span>
                </span>
              </RouterLink>
              <div v-if="auth.user" class="user-brief">
                <NAvatar v-if="auth.user.avatar_url" round :size="28" :src="auth.user.avatar_url" />
                <span class="user-name">{{ auth.user.display_name }}</span>
                <NTooltip>
                  <template #trigger>
                    <NButton quaternary circle size="small" aria-label="退出登录" @click="logout">
                      <template #icon
                        ><NIcon><LogOutOutline /></NIcon
                      ></template>
                    </NButton>
                  </template>
                  退出登录
                </NTooltip>
              </div>
              <ThemeToggle />
            </div>
          </header>
          <main :class="{ full: isConversation || isSharedPage, 'auth-main': isAuthPage }">
            <RouterView />
          </main>
        </div>
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>

<style scoped>
.app {
  height: 100%;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.topnav {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  padding: 0 24px;
  height: 58px;
  background: color-mix(in srgb, var(--bg-soft) 82%, transparent);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--border-soft);
  flex-shrink: 0;
}

/* 品牌：灯笼点 + 中文衬线 + 英文小字 */
.brand {
  display: flex;
  align-items: baseline;
  gap: 9px;
  color: var(--text);
}

.lantern {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 10px 2px var(--accent-glow);
  align-self: center;
}

.brand-cn {
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 4px;
}

.brand-en {
  font-size: 12px;
  color: var(--text-faint);
  letter-spacing: 1px;
}

.links {
  display: flex;
  gap: 4px;
  justify-content: center;
}

.nav-link {
  position: relative;
  color: var(--text-dim);
  padding: 6px 14px;
  font-size: 13.5px;
  letter-spacing: 1.5px;
  transition: color 0.2s ease;
}

/* 下划线饰条从中间展开 */
.nav-link::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: 0;
  width: 0;
  height: 2px;
  background: var(--accent);
  transform: translateX(-50%);
  transition: width 0.25s ease;
  border-radius: 1px;
}

.nav-link:hover {
  color: var(--text);
}

.nav-link.router-link-exact-active,
.nav-link.section-active {
  color: var(--accent);
}

.nav-link.router-link-exact-active::after,
.nav-link.section-active::after {
  width: calc(100% - 24px);
}

.nav-right {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 16px;
}

.account-brief {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 0;
}

.user-brief {
  display: flex;
  align-items: center;
  gap: 7px;
  padding-left: 12px;
  border-left: 1px solid var(--border-soft);
}

.user-name {
  max-width: 120px;
  overflow: hidden;
  color: var(--text-dim);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  background: color-mix(in srgb, var(--accent-soft) 30%, transparent);
  border-radius: 6px;
  font-size: 13px;
  cursor: help;
  transition: background 0.2s ease;
}

.stat-item:hover {
  background: color-mix(in srgb, var(--accent-soft) 50%, transparent);
}

.stat-icon {
  font-size: 14px;
  line-height: 1;
}

.stat-name {
  color: var(--text-dim);
}

.stat-value {
  font-weight: 600;
  color: var(--text);
  letter-spacing: 0.5px;
}

main {
  flex: 1;
  min-height: 0;
  min-width: 0;
  overflow: auto;
  padding: 28px 24px;
}

main.full {
  padding: 0;
  overflow: hidden;
}

main.auth-main {
  padding: 0;
  overflow: auto;
}

/* 移动端适配 */
@media (max-width: 768px) {
  .topnav {
    grid-template-areas:
      'brand account'
      'links links';
    grid-template-columns: minmax(0, 1fr) auto;
    height: auto;
    min-height: calc(88px + env(safe-area-inset-top));
    padding: env(safe-area-inset-top) max(12px, env(safe-area-inset-right)) 0
      max(12px, env(safe-area-inset-left));
    column-gap: 8px;
    row-gap: 0;
  }

  .brand {
    grid-area: brand;
    min-width: 0;
    align-self: center;
  }

  .brand-cn {
    font-size: 15px;
    letter-spacing: 2px;
  }

  .brand-en {
    display: none; /* 移动端隐藏英文副标题 */
  }

  .links {
    grid-area: links;
    align-self: end;
    gap: 2px;
    justify-content: flex-start;
    overflow-x: auto;
    scrollbar-width: none; /* Firefox */
  }

  .nav-right {
    grid-area: account;
    align-self: center;
    gap: 4px;
  }

  .links::-webkit-scrollbar {
    display: none; /* Chrome/Safari */
  }

  .nav-link {
    display: grid;
    min-height: 40px;
    padding: 6px 12px;
    place-items: center;
    font-size: 12px;
    letter-spacing: 1px;
    white-space: nowrap;
  }

  .account-brief {
    display: none;
  }

  .user-brief {
    gap: 3px;
    padding-left: 0;
    border-left: 0;
  }

  .user-name {
    display: none;
  }

  main {
    padding: 16px max(12px, env(safe-area-inset-right)) calc(16px + env(safe-area-inset-bottom))
      max(12px, env(safe-area-inset-left));
  }
}

@media (max-width: 380px) {
  .brand {
    gap: 6px;
  }

  .brand-cn {
    font-size: 14px;
    letter-spacing: 1px;
  }
}
</style>
