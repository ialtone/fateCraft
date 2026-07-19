<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { NConfigProvider, NDialogProvider, NMessageProvider } from 'naive-ui'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { useThemeStore } from '@/stores/theme'
import { useAccountStore } from '@/stores/account'

const route = useRoute()
const theme = useThemeStore()
const account = useAccountStore()

// 主对话页使用全屏三栏布局，不显示顶部导航。
const isConversation = computed(() => route.name === 'conversation')

const navs = [
  { to: '/', label: '对话' },
  { to: '/characters', label: '角色卡' },
  { to: '/worldbooks', label: '世界书' },
  { to: '/prompts', label: 'Prompt' },
  { to: '/settings', label: '设置' },
]

// 加载账号数据
onMounted(() => {
  account.load().catch(() => {})
})
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
          <header v-if="!isConversation" class="topnav">
            <RouterLink to="/" class="brand">
              <span class="lantern" aria-hidden="true"></span>
              <span class="brand-cn">命运茶馆</span>
              <span class="brand-en">Fatecraft</span>
            </RouterLink>
            <nav class="links">
              <RouterLink v-for="n in navs" :key="n.to" :to="n.to" class="nav-link">
                {{ n.label }}
              </RouterLink>
            </nav>
            <div class="nav-right">
              <!-- 未加载完成时显示加载态，不展示假数据 -->
              <div v-if="account.loaded" class="account-brief">
                <span class="stat-item" title="累计灵石">
                  <span class="stat-icon">💎</span>
                  <span class="stat-value">{{ account.totalSpiritStones.toLocaleString() }}</span>
                </span>
                <span class="stat-item" title="剩余天道回溯">
                  <span class="stat-icon">🔄</span>
                  <span class="stat-value">{{ account.rewindCount }}</span>
                </span>
              </div>
              <ThemeToggle />
            </div>
          </header>
          <main :class="{ full: isConversation }">
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
  display: flex;
  flex-direction: column;
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

.nav-link.router-link-exact-active {
  color: var(--accent);
}

.nav-link.router-link-exact-active::after {
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

.stat-value {
  font-family: 'Courier New', Consolas, monospace;
  font-weight: 600;
  color: var(--text);
  letter-spacing: 0.5px;
}

main {
  flex: 1;
  overflow: auto;
  padding: 28px 24px;
}

main.full {
  padding: 0;
  overflow: hidden;
}

/* 移动端适配 */
@media (max-width: 768px) {
  .topnav {
    grid-template-columns: auto 1fr auto;
    padding: 0 12px;
    height: 52px;
    gap: 8px;
  }

  .brand-cn {
    font-size: 15px;
    letter-spacing: 2px;
  }

  .brand-en {
    display: none; /* 移动端隐藏英文副标题 */
  }

  .links {
    gap: 2px;
    justify-content: flex-start;
    overflow-x: auto;
    scrollbar-width: none; /* Firefox */
  }

  .links::-webkit-scrollbar {
    display: none; /* Chrome/Safari */
  }

  .nav-link {
    padding: 6px 10px;
    font-size: 12px;
    letter-spacing: 1px;
    white-space: nowrap;
  }

  .account-brief {
    gap: 8px;
  }

  .stat-item {
    padding: 3px 8px;
    font-size: 12px;
  }

  .stat-icon {
    font-size: 13px;
  }

  main {
    padding: 16px 12px;
  }
}
</style>
