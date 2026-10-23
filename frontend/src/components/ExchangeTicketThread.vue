<script setup lang="ts">
import type { RewardExchangeTicket } from '@/types/rewards'
import MarkdownEditor from './MarkdownEditor.vue'
defineProps<{ ticket: RewardExchangeTicket }>()
const kinds = { request: '用户申请', reject: '已驳回', refund: '已退款' }
</script>
<template>
  <ol class="thread">
    <li v-for="entry in ticket.entries" :key="entry.id">
      <small>{{ kinds[entry.kind] }} · {{ new Date(entry.created_at).toLocaleString() }}</small>
      <MarkdownEditor :model-value="entry.body" readonly />
    </li>
  </ol>
</template>
<style scoped>
.thread {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
li {
  padding: 8px 10px;
  border-left: 3px solid var(--border-soft);
}
small {
  color: var(--text-dim);
}
</style>
