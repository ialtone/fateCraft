// 亮/暗主题状态：持久化到 localStorage，并同步到 <html data-theme> 供自定义 CSS 变量切换。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { darkTheme } from 'naive-ui'
import type { GlobalTheme, GlobalThemeOverrides } from 'naive-ui'
import { darkOverrides, lightOverrides } from '@/theme'

export type ThemeMode = 'dark' | 'light'

const STORAGE_KEY = 'gft-theme'

function initialMode(): ThemeMode {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved === 'dark' || saved === 'light') return saved
  return 'dark' // 夜茶馆是默认形态
}

export const useThemeStore = defineStore('theme', () => {
  const mode = ref<ThemeMode>(initialMode())

  function apply() {
    document.documentElement.dataset.theme = mode.value
  }
  apply()

  function toggle() {
    mode.value = mode.value === 'dark' ? 'light' : 'dark'
    localStorage.setItem(STORAGE_KEY, mode.value)
    apply()
  }

  // Naive UI 的主题对象：暗色用 darkTheme，亮色用 null（默认亮色基底）。
  const naiveTheme = computed<GlobalTheme | null>(() => (mode.value === 'dark' ? darkTheme : null))
  const overrides = computed<GlobalThemeOverrides>(() =>
    mode.value === 'dark' ? darkOverrides : lightOverrides,
  )

  return { mode, toggle, naiveTheme, overrides }
})
