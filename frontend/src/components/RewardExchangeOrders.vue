<script setup lang="ts">
import { ref, watch } from 'vue'
import { NAlert, NButton, NEmpty, NInput, NModal, NSpin } from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import type { RewardExchangeOrder, RewardExchangeTicket } from '@/types/rewards'
import ExchangeResult from './ExchangeResult.vue'
import MarkdownEditor from './MarkdownEditor.vue'
import ExchangeTicketThread from './ExchangeTicketThread.vue'
import { rewardError } from '@/lib/rewards'
import { validExchangeRequestID } from '@/lib/rewardExchangeState'
const props = defineProps<{ admin?: boolean; refreshKey?: number }>()
const emit = defineEmits<{ confirmed: [order: RewardExchangeOrder] }>()
const lookupOpen = ref(false),
  lookupID = ref(''),
  lookupError = ref(''),
  lookupBusy = ref(false),
  lookupResult = ref<RewardExchangeOrder>()
watch(lookupID, () => {
  lookupResult.value = undefined
  lookupError.value = ''
})
const rows = ref<RewardExchangeOrder[]>([]),
  cursor = ref(''),
  userID = ref(''),
  error = ref(''),
  loading = ref(false),
  detail = ref<RewardExchangeOrder>(),
  open = ref(false)
let sequence = 0
let appliedUserID: string | undefined
async function load(more = false) {
  const token = ++sequence
  loading.value = true
  error.value = ''
  try {
    const filterUser = more ? appliedUserID : userID.value.trim() || undefined
    const result = await api.orders(!!props.admin, more ? cursor.value : undefined, filterUser)
    if (token !== sequence) return
    appliedUserID = filterUser
    rows.value = more ? [...rows.value, ...result.orders] : result.orders
    cursor.value = result.next_cursor
  } catch (err) {
    if (token === sequence) error.value = rewardError(err)
  } finally {
    if (token === sequence) loading.value = false
  }
}
watch(
  () => props.refreshKey,
  () => load(),
  { immediate: true },
)
const ticket = ref<RewardExchangeTicket>(),
  ticketsEnabled = ref(false),
  ticketBody = ref(''),
  ticketBusy = ref(false),
  ticketError = ref('')
const ticketLabels = { open: '工单处理中', rejected: '工单已驳回', refunded: '已退款' }
async function showOrder(row: RewardExchangeOrder) {
  detail.value = row
  open.value = true
  ticket.value = undefined
  ticketBody.value = ticketError.value = ''
  if (props.admin || row.snapshot.kind !== 'cdk') return
  try {
    const result = await api.tickets()
    ticketsEnabled.value = result.tickets_enabled
    ticket.value = result.tickets.find((t) => t.order_id === row.id)
  } catch (err) {
    ticketError.value = rewardError(err)
  }
}
async function submitTicket() {
  if (!detail.value || !ticketBody.value.trim()) return
  ticketBusy.value = true
  ticketError.value = ''
  try {
    ticket.value = await api.openTicket(detail.value.id, ticketBody.value.trim())
    detail.value.ticket_status = ticket.value.status
    ticketBody.value = ''
  } catch (err) {
    ticketError.value = rewardError(err)
  } finally {
    ticketBusy.value = false
  }
}
async function lookup() {
  if (props.admin || lookupBusy.value) return
  lookupError.value = ''
  lookupResult.value = undefined
  if (!validExchangeRequestID(lookupID.value)) {
    lookupError.value = '请输入完整的兑换请求 ID。'
    return
  }
  lookupBusy.value = true
  try {
    const order = await api.request(lookupID.value.trim())
    lookupResult.value = order
    emit('confirmed', order)
  } catch (err) {
    lookupError.value =
      (err as { response?: { status: number } }).response?.status === 404
        ? '暂未查到这笔兑换；处理中或网络异常时，请保留原请求并稍后查询，不要重新创建兑换。'
        : rewardError(err)
  } finally {
    lookupBusy.value = false
  }
}
function viewLookupDetail() {
  if (!lookupResult.value) return
  lookupOpen.value = false
  showOrder(lookupResult.value)
}
</script>
<template>
  <section class="orders">
    <div class="toolbar">
      <div>
        <h3>{{ admin ? '兑换订单' : '兑换记录' }}</h3>
        <p v-if="!admin" class="intro">查看已完成的兑换；结果不确定时，可通过原请求查询。</p>
      </div>
      <div class="toolbar-actions">
        <NButton v-if="!admin" @click="lookupOpen = true">查询兑换结果</NButton>
        <NButton :loading="loading" @click="load()">刷新记录</NButton>
      </div>
    </div>
    <div v-if="admin" class="toolbar">
      <NInput
        v-model:value="userID"
        placeholder="按用户 ID 筛选（留空查看全部）"
        @keyup.enter="load()"
      /><NButton @click="load()">查询</NButton>
    </div>
    <NAlert v-if="error" type="error">{{ error }}</NAlert
    ><NSpin :show="loading && !rows.length">
      <article v-for="row in rows" :key="row.id">
        <div>
          <strong>{{ row.snapshot.name }} × {{ row.units }}</strong>
          <p>
            支付 {{ row.cost_quantity }} 积分<template v-if="row.snapshot.kind !== 'cdk'">
              → 获得 {{ row.reward_quantity }} 次剧情改写</template
            >
          </p>
          <small
            >{{ new Date(row.created_at).toLocaleString() }} · 已完成<span v-if="row.ticket_status">
              · {{ ticketLabels[row.ticket_status] }}</span
            ><span v-if="admin" class="user">
              · 用户 @{{ row.username || row.user_id }}</span
            ></small
          >
        </div>
        <NButton @click="showOrder(row)">详情</NButton>
      </article>
      <NEmpty
        v-if="!loading && !error && !rows.length"
        :description="admin ? '暂无兑换订单' : '还没有兑换记录'" /></NSpin
    ><NButton v-if="cursor" :loading="loading" @click="load(true)">加载更多</NButton>
    <NModal
      v-model:show="open"
      preset="card"
      title="兑换详情"
      style="width: min(640px, 94vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
      ><div v-if="detail" class="detail">
        <h3>{{ detail.snapshot.name }} × {{ detail.units }}</h3>
        <p>{{ detail.snapshot.description }}</p>
        <p>
          支付 {{ detail.cost_quantity }} 积分<template v-if="detail.snapshot.kind !== 'cdk'"
            >；获得 {{ detail.reward_quantity }} 次剧情改写</template
          >
        </p>
        <p>兑换时间：{{ new Date(detail.created_at).toLocaleString() }}</p>
        <ExchangeResult :order="detail" />
        <section v-if="!admin && detail.snapshot.kind === 'cdk'" class="ticket">
          <h4>售后工单{{ ticket ? ` · ${ticketLabels[ticket.status]}` : '' }}</h4>
          <ExchangeTicketThread v-if="ticket" :ticket="ticket" />
          <template v-if="ticketsEnabled && (!ticket || ticket.status === 'rejected')">
            <MarkdownEditor
              v-model="ticketBody"
              :maxlength="2000"
              placeholder="CDK 无法使用？请描述问题（支持 Markdown）"
            /><NButton :loading="ticketBusy" :disabled="!ticketBody.trim()" @click="submitTicket">{{
              ticket ? '补充说明并重新提交' : '提交工单'
            }}</NButton>
          </template>
          <NAlert v-if="ticketError" type="error">{{ ticketError }}</NAlert>
        </section>
        <details>
          <summary>交易编号</summary>
          <p>订单：{{ detail.id }}</p>
          <p>请求：{{ detail.request_id }}</p>
          <template v-if="admin">
            <p>商品修订：{{ detail.offer_revision }}</p>
            <p>支付流水：{{ detail.cost_ledger_id }}</p>
            <p>所得流水：{{ detail.reward_ledger_id }}</p>
          </template>
        </details>
      </div></NModal
    >
    <NModal
      v-model:show="lookupOpen"
      preset="card"
      title="查询兑换结果"
      :mask-closable="!lookupBusy"
      :closable="!lookupBusy"
      :close-on-esc="!lookupBusy"
      style="width: min(620px, 94vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
    >
      <p>填写兑换确认窗口中保留的请求 ID，仅查询当前账号的兑换。</p>
      <form class="lookup-form" @submit.prevent="lookup">
        <NInput
          v-model:value="lookupID"
          :disabled="lookupBusy"
          :maxlength="36"
          aria-label="兑换请求 ID"
          placeholder="完整兑换请求 ID"
        />
        <NButton
          attr-type="submit"
          type="primary"
          :loading="lookupBusy"
          :disabled="!lookupID.trim()"
          >查询</NButton
        >
      </form>
      <NAlert v-if="lookupError" type="error">{{ lookupError }}</NAlert>
      <div v-if="lookupResult" class="lookup-result">
        <NAlert type="success"
          >已确认兑换完成：{{ lookupResult.snapshot.name }} × {{ lookupResult.units }}</NAlert
        >
        <p>
          支付 {{ lookupResult.cost_quantity }} 积分；获得
          {{ lookupResult.reward_quantity }} 次剧情改写。
        </p>
        <NButton @click="viewLookupDetail">查看兑换详情</NButton>
      </div>
    </NModal>
  </section>
</template>
<style scoped>
.orders {
  display: grid;
  gap: 14px;
}
.toolbar,
article {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
}
.toolbar-actions,
.lookup-form {
  display: flex;
  gap: 10px;
}
.toolbar-actions {
  flex-wrap: wrap;
}
.lookup-form {
  margin: 16px 0;
}
.lookup-result {
  display: grid;
  gap: 12px;
  margin-top: 16px;
}
.intro {
  margin: 6px 0 0;
  font-size: 13px;
}
summary {
  cursor: pointer;
  color: var(--text-dim);
}
article {
  border-bottom: 1px solid var(--border-soft);
  padding: 14px 0;
}
p,
small {
  color: var(--text-dim);
}
.user {
  font-size: 14px;
}
.detail {
  overflow-wrap: anywhere;
}
.ticket {
  display: grid;
  gap: 10px;
  margin-top: 12px;
}
h3 {
  margin: 0;
}
@media (max-width: 600px) {
  article,
  .toolbar {
    align-items: start;
    flex-direction: column;
  }
}
</style>
