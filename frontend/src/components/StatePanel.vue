<script setup lang="ts">
import StateTree from './StateTree.vue'
import { useStateStore } from '@/stores/state'

defineProps<{ convId: string }>()
const stateStore = useStateStore()
</script>

<template>
  <aside class="state-panel">
    <div class="head">
      <span class="title">角色状态</span>
    </div>
    <div class="body scroll">
      <div
        v-if="!stateStore.state || Object.keys(stateStore.state).length === 0"
        class="empty-state"
      >
        <div class="empty-icon">📋</div>
        <div class="empty-text">暂无状态数据</div>
        <div class="empty-hint">开始对话后，角色状态会在这里显示</div>
      </div>
      <StateTree v-else :data="stateStore.state" />
    </div>
  </aside>
</template>

<style scoped>
.state-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
  min-width: 260px;
  max-width: 320px;
}

.head {
  padding: 16px 18px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  background: var(--bg-soft);
}

.title {
  font-weight: 600;
  font-size: 14px;
  color: var(--text);
  letter-spacing: 0.5px;
}

.body {
  padding: 16px;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

.body::-webkit-scrollbar {
  width: 6px;
}

.body::-webkit-scrollbar-track {
  background: transparent;
}

.body::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 3px;
}

.body::-webkit-scrollbar-thumb:hover {
  background: var(--border-strong);
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 20px;
  text-align: center;
  min-height: 200px;
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
  opacity: 0.5;
}

.empty-text {
  font-size: 14px;
  font-weight: 500;
  color: var(--text-faint);
  margin-bottom: 8px;
}

.empty-hint {
  font-size: 12px;
  color: var(--text-faint);
  opacity: 0.7;
  line-height: 1.5;
}
</style>
