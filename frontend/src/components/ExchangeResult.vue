<script setup lang="ts">
import { NButton, useMessage } from 'naive-ui'
import type { RewardExchangeOrder } from '@/types/rewards'
import MarkdownEditor from './MarkdownEditor.vue'
// 订单快照中的 CDK 与使用教程；成功弹窗与兑换详情共用。
const props = defineProps<{ order: RewardExchangeOrder }>()
const message = useMessage()
async function copy(code: string) {
  try {
    await navigator.clipboard.writeText(code)
    message.success('已复制')
  } catch {
    message.error('复制失败，请手动选择复制')
  }
}
function downloadCodes() {
  const codes = props.order.snapshot.codes ?? []
  if (!codes.length) return
  const url = URL.createObjectURL(
    new Blob([codes.join('\n')], { type: 'text/plain;charset=utf-8' }),
  )
  const link = document.createElement('a')
  link.href = url
  link.download = '兑换码.txt'
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
</script>
<template>
  <div class="result">
    <section v-if="order.snapshot.codes?.length">
      <div class="codes-toolbar">
        <span>兑换码 · 共 {{ order.snapshot.codes.length }} 个</span>
        <div class="codes-actions">
          <NButton size="small" @click="copy(order.snapshot.codes.join('\n'))">全部复制</NButton>
          <NButton size="small" @click="downloadCodes">下载 TXT</NButton>
        </div>
      </div>
      <div class="codes-list" role="region" aria-label="兑换码列表" tabindex="0">
        <div v-for="(code, index) in order.snapshot.codes" :key="code" class="cdk">
          <span class="code-index">{{ index + 1 }}</span>
          <code>{{ code }}</code>
          <NButton size="small" @click="copy(code)">复制</NButton>
        </div>
      </div>
    </section>
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
  display: grid;
  grid-template-columns: 3ch minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
  box-sizing: border-box;
  height: 40px;
  padding: 0 10px;
}
.cdk + .cdk {
  border-top: 1px solid var(--border-soft);
}
.codes-list {
  box-sizing: border-box;
  max-height: min(402px, 50dvh);
  overflow: auto;
  background: var(--bg-soft);
  border: 1px solid var(--border-soft);
  border-radius: 6px;
}
.codes-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
.codes-actions {
  display: flex;
  gap: 8px;
}
.code-index {
  color: var(--text-dim);
  font-size: 12px;
  text-align: right;
}
code {
  overflow-x: auto;
  white-space: pre;
  user-select: all;
}
h4 {
  margin: 0 0 6px;
}
</style>
