<script setup lang="ts">
// 亮/暗模式切换按钮：日月图标随主题翻转。
import { NButton, NIcon, NTooltip } from 'naive-ui'
import { MoonOutline, SunnyOutline } from '@vicons/ionicons5'
import { useThemeStore } from '@/stores/theme'

const theme = useThemeStore()
</script>

<template>
  <NTooltip placement="bottom">
    <template #trigger>
      <NButton quaternary circle aria-label="切换亮暗模式" @click="theme.toggle()">
        <template #icon>
          <Transition name="flip" mode="out-in">
            <NIcon v-if="theme.mode === 'dark'" :component="MoonOutline" />
            <NIcon v-else :component="SunnyOutline" />
          </Transition>
        </template>
      </NButton>
    </template>
    {{ theme.mode === 'dark' ? '切换到宣纸（亮色）' : '切换到夜茶馆（暗色）' }}
  </NTooltip>
</template>

<style scoped>
.flip-enter-active,
.flip-leave-active {
  transition:
    transform 0.25s ease,
    opacity 0.25s ease;
}
.flip-enter-from {
  transform: rotate(-90deg) scale(0.6);
  opacity: 0;
}
.flip-leave-to {
  transform: rotate(90deg) scale(0.6);
  opacity: 0;
}
</style>
