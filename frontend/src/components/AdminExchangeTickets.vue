<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NModal,
  NSelect,
  NSwitch,
  NTag,
  useMessage,
} from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import { rewardError } from '@/lib/rewards'
import type { RewardExchangeTicket } from '@/types/rewards'
import ExchangeResult from './ExchangeResult.vue'
import ExchangeTicketThread from './ExchangeTicketThread.vue'
const message = useMessage()
const labels = { open: '待处理', rejected: '已驳回', refunded: '已退款' }
const tagTypes = { open: 'warning', rejected: 'default', refunded: 'success' } as const
const statusOptions = [
  { label: '全部', value: '' },
  ...Object.entries(labels).map(([value, label]) => ({ label, value })),
]
const tickets = ref<RewardExchangeTicket[]>([]),
  enabled = ref(true),
  status = ref('open'),
  reason = ref(''),
  selected = ref<RewardExchangeTicket>(),
  busy = ref(false),
  error = ref('')
const userText = (t: RewardExchangeTicket) => '@' + (t.username || t.user_id)
const latest = (t: RewardExchangeTicket) => t.entries[t.entries.length - 1]?.body ?? ''
const time = (value: string) => new Date(value).toLocaleString()
const resolving = computed(() => selected.value?.status === 'open')
async function run(action: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  try {
    await action()
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
const load = () =>
  run(async () => {
    const result = await api.tickets(true, status.value || undefined)
    tickets.value = result.tickets
    enabled.value = result.tickets_enabled
  })
const toggle = (on: boolean) =>
  run(async () => {
    await api.setTicketsEnabled(on)
    enabled.value = on
  })
function open(ticket: RewardExchangeTicket) {
  selected.value = ticket
  reason.value = ''
  error.value = ''
}
const resolve = (approve: boolean) =>
  run(async () => {
    const ticket = selected.value!
    selected.value = await api.resolveTicket(ticket.id, approve, reason.value)
    reason.value = ''
    message.success(approve ? '已退款，积分与兑换额度已返还' : '已驳回')
    const result = await api.tickets(true, status.value || undefined)
    tickets.value = result.tickets
    // 列表带有用户名与订单，优先用列表中的最新数据刷新弹窗。
    selected.value = result.tickets.find((t) => t.id === ticket.id) ?? {
      ...selected.value,
      username: ticket.username,
      order: ticket.order,
    }
  })
watch(status, load, { immediate: true })
</script>
<template>
  <section class="tickets">
    <div class="toolbar">
      <label class="toggle"
        ><NSwitch :value="enabled" :loading="busy" @update:value="toggle" /><span
          >允许玩家提交工单<small>关闭后玩家无法新建工单，已有工单仍可处理</small></span
        ></label
      >
      <NSelect v-model:value="status" :options="statusOptions" style="width: 140px" />
    </div>
    <NAlert v-if="error && !selected" type="error">{{ error }}</NAlert>
    <div class="list">
      <button v-for="ticket in tickets" :key="ticket.id" class="row" @click="open(ticket)">
        <strong>{{ ticket.order?.snapshot.name }} × {{ ticket.order?.units }}</strong>
        <span class="user">{{ userText(ticket) }}</span>
        <code>{{ ticket.order_id.slice(0, 8) }}</code>
        <NTag size="small" :type="tagTypes[ticket.status]">{{ labels[ticket.status] }}</NTag>
        <span class="muted">{{ time(ticket.updated_at) }}</span>
        <span class="snippet muted">{{ latest(ticket) }}</span>
      </button>
    </div>
    <NEmpty v-if="!busy && !tickets.length" description="暂无工单" />
    <NModal
      :show="!!selected"
      preset="card"
      :title="`工单 · ${selected?.order?.snapshot.name ?? ''}`"
      :mask-closable="!busy"
      :closable="!busy"
      style="width: min(720px, 94vw)"
      @update:show="(v: boolean) => !v && (selected = undefined)"
      ><div v-if="selected" class="detail">
        <NAlert v-if="error" type="error">{{ error }}</NAlert>
        <dl>
          <dt>状态</dt>
          <dd>
            <NTag size="small" :type="tagTypes[selected.status]">{{
              labels[selected.status]
            }}</NTag>
          </dd>
          <dt>用户</dt>
          <dd>{{ userText(selected) }}</dd>
          <dt>订单</dt>
          <dd>
            <code>{{ selected.order_id }}</code>
          </dd>
          <dt>兑换</dt>
          <dd>
            {{ selected.order?.snapshot.name }} × {{ selected.order?.units }} · 支付
            {{ selected.order?.cost_quantity }} 积分 ·
            {{ selected.order ? time(selected.order.created_at) : '' }}
          </dd>
        </dl>
        <ExchangeResult v-if="selected.order" :order="selected.order" />
        <h4>对话</h4>
        <ExchangeTicketThread :ticket="selected" />
        <div v-if="resolving" class="actions">
          <NInput
            v-model:value="reason"
            type="textarea"
            :rows="2"
            :maxlength="2000"
            placeholder="驳回原因 / 备注（驳回时必填）"
          />
          <div class="buttons">
            <NButton :loading="busy" :disabled="!reason.trim()" @click="resolve(false)"
              >驳回</NButton
            ><NButton type="primary" :loading="busy" @click="resolve(true)">同意退款</NButton>
          </div>
        </div>
      </div></NModal
    >
  </section>
</template>
<style scoped>
.tickets,
.detail {
  display: grid;
  gap: 12px;
}
.toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
}
.toggle {
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
}
.toggle span {
  display: grid;
  gap: 2px;
}
.list {
  display: grid;
}
.row {
  display: grid;
  grid-template-columns: minmax(140px, 1.4fr) minmax(90px, 1fr) auto auto auto minmax(0, 2fr);
  gap: 12px;
  align-items: center;
  padding: 10px 8px;
  border: 0;
  border-bottom: 1px solid var(--border-soft);
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.row:hover {
  background: var(--bg-soft);
}
.user,
.muted {
  color: var(--text-dim);
  font-size: 14px;
}
.snippet {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 8px 16px;
  margin: 0;
  overflow-wrap: anywhere;
}
dt {
  color: var(--text-dim);
}
dd,
h4 {
  margin: 0;
}
.actions {
  display: grid;
  gap: 8px;
}
.buttons {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
small {
  color: var(--text-dim);
}
@media (max-width: 720px) {
  .row {
    grid-template-columns: 1fr auto;
  }
  .snippet {
    grid-column: 1 / -1;
  }
}
</style>
