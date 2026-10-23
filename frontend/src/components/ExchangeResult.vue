<script setup lang="ts">
import { NButton, useMessage } from 'naive-ui'
import type { RewardExchangeOrder } from '@/types/rewards'
import MarkdownEditor from './MarkdownEditor.vue'
// 订单快照中的 CDK 与使用教程；成功弹窗与兑换详情共用。
defineProps<{ order: RewardExchangeOrder }>()
const message = useMessage()
async function copy(code: string) {
  try {
    await navigator.clipboard.writeText(code)
    message.success('已复制')
  } catch {
    message.error('复制失败，请手动选择复制')
  }
}
</script>
<template>
  <div class="result">
    <div v-for="code in order.snapshot.codes ?? []" :key="code" class="cdk">
      <code>{{ code }}</code
      ><NButton size="small" @click="copy(code)">复制</NButton>
    </div>
    <section v-if="order.snapshot.tutorial?.trim()">
      <h4>使用教程</h4>
      <MarkdownEditor :model-value="order.snapshot.tutorial" readonly />
    </section>
  </div>
</template>
<style scoped>
.result {
  display: grid;
  gap: 12px;
  margin: 12px 0;
}
.cdk {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  background: var(--bg-soft);
  border: 1px dashed var(--border-soft);
  border-radius: 6px;
}
code {
  overflow-wrap: anywhere;
  user-select: all;
}
h4 {
  margin: 0 0 6px;
}
</style>
