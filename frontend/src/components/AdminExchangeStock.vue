<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NCheckbox,
  NEmpty,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpin,
  useMessage,
} from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import { rewardError } from '@/lib/rewards'
import type { RewardExchangeOffer, RewardExchangeOrder, RewardExchangeStock } from '@/types/rewards'
import ExchangeResult from './ExchangeResult.vue'
const props = defineProps<{ offer: RewardExchangeOffer }>()
const message = useMessage()
const statusLabels: Record<string, string> = {
  available: '可兑换',
  redeemed: '已兑换',
  removed: '已移除',
  voided: '已作废',
}
const statusOptions = [
  { label: '全部状态', value: '' },
  ...Object.entries(statusLabels).map(([value, label]) => ({ label, value })),
]
const rows = ref<RewardExchangeStock[]>([]),
  more = ref(false),
  status = ref(''),
  codes = ref(''),
  quantity = ref<number | null>(1),
  selected = ref<string[]>([]),
  duplicates = ref<string[]>([]),
  busy = ref(false),
  error = ref(''),
  orderOpen = ref(false),
  orderBusy = ref(false),
  orderError = ref(''),
  order = ref<RewardExchangeOrder>()
function timeText(value: string) {
  return new Date(value).toLocaleString()
}
async function showOrder(id: string) {
  orderOpen.value = true
  order.value = undefined
  orderError.value = ''
  orderBusy.value = true
  try {
    order.value = await api.adminOrder(id)
  } catch (err) {
    orderError.value = rewardError(err)
  } finally {
    orderBusy.value = false
  }
}
async function load(next = false) {
  busy.value = true
  error.value = ''
  try {
    const page = await api.stock(props.offer.id, {
      status: status.value || undefined,
      offset: next ? rows.value.length : 0,
    })
    rows.value = next ? [...rows.value, ...page.stock] : page.stock
    more.value = page.has_more
    if (!next) selected.value = []
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
async function add() {
  busy.value = true
  error.value = ''
  duplicates.value = []
  try {
    const body =
      props.offer.kind === 'cdk' ? { codes: codes.value } : { quantity: quantity.value ?? 0 }
    message.success(`已添加 ${await api.addStock(props.offer.id, body)} 份库存`)
    codes.value = ''
    await load()
  } catch (err) {
    error.value = rewardError(err)
    duplicates.value =
      (err as { response?: { data?: { duplicates?: string[] } } }).response?.data?.duplicates ?? []
  } finally {
    busy.value = false
  }
}
async function remove() {
  busy.value = true
  error.value = ''
  try {
    message.success(`已移除 ${await api.removeStock(props.offer.id, selected.value)} 份`)
    await load()
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
function toggle(id: string, on: boolean) {
  selected.value = on ? [...selected.value, id] : selected.value.filter((s) => s !== id)
}
watch(status, () => load(), { immediate: true })
</script>
<template>
  <div class="stock">
    <div v-if="offer.kind === 'cdk'" class="add">
      <NInput
        v-model:value="codes"
        type="textarea"
        :rows="5"
        placeholder="粘贴 CDK，每行一个；重复的 CDK 会被拒绝"
      /><NButton type="primary" :loading="busy" :disabled="!codes.trim()" @click="add"
        >导入 CDK</NButton
      >
    </div>
    <div v-else class="add inline">
      <NInputNumber
        v-model:value="quantity"
        :min="1"
        :max="5000"
        :precision="0"
        update-value-on-input
        style="width: 160px"
      /><NButton type="primary" :loading="busy" :disabled="!quantity" @click="add"
        >增加库存</NButton
      >
    </div>
    <NAlert v-if="error" type="error"
      >{{ error }}
      <p v-if="duplicates.length">重复的 CDK：{{ duplicates.join('、') }}</p></NAlert
    >
    <div class="add inline">
      <NSelect v-model:value="status" :options="statusOptions" style="width: 160px" /><NButton
        :disabled="!selected.length"
        :loading="busy"
        @click="remove"
        >移除选中（{{ selected.length }}）</NButton
      >
    </div>
    <div v-for="row in rows" :key="row.id" class="row">
      <NCheckbox
        :disabled="row.status !== 'available'"
        :checked="selected.includes(row.id)"
        @update:checked="(on: boolean) => toggle(row.id, on)"
      />
      <code class="code">{{ row.code || '—' }}</code>
      <span>{{ statusLabels[row.status] }}</span>
      <span class="meta">{{ row.user_id ? `用户 @${row.username || row.user_id}` : '—' }}</span>
      <span class="meta">{{
        row.redeemed_at ? `兑换 ${timeText(row.redeemed_at)}` : `入库 ${timeText(row.created_at)}`
      }}</span>
      <NButton
        v-if="row.order_id"
        text
        type="primary"
        class="order"
        :title="row.order_id"
        @click="showOrder(row.order_id)"
        >订单 {{ row.order_id.slice(0, 8) }}</NButton
      >
    </div>
    <NEmpty v-if="!busy && !rows.length" description="暂无库存" />
    <NButton v-if="more" :loading="busy" @click="load(true)">加载更多</NButton>
    <NModal
      v-model:show="orderOpen"
      preset="card"
      title="兑换订单"
      style="width: min(640px, 94vw)"
    >
      <NSpin :show="orderBusy">
        <NAlert v-if="orderError" type="error">{{ orderError }}</NAlert>
        <div v-else-if="order" class="order-detail">
          <h3>{{ order.snapshot.name }} × {{ order.units }}</h3>
          <p>{{ order.snapshot.description }}</p>
          <p>用户：@{{ order.username || order.user_id }}</p>
          <p>
            支付 {{ order.cost_quantity }} 积分<template v-if="order.snapshot.kind !== 'cdk'"
              >；获得 {{ order.reward_quantity }} 次剧情改写</template
            >
          </p>
          <p>兑换时间：{{ timeText(order.created_at) }}</p>
          <ExchangeResult :order="order" />
          <details>
            <summary>交易编号</summary>
            <p>订单：{{ order.id }}</p>
            <p>请求：{{ order.request_id }}</p>
            <p>商品修订：{{ order.offer_revision }}</p>
            <p>支付流水：{{ order.cost_ledger_id }}</p>
            <p>所得流水：{{ order.reward_ledger_id }}</p>
          </details>
        </div>
      </NSpin>
    </NModal>
  </div>
</template>
<style scoped>
.stock,
.add {
  display: grid;
  gap: 12px;
}
.inline {
  display: flex;
  align-items: center;
}
.row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto auto auto;
  gap: 12px;
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid var(--border-soft);
  overflow-wrap: anywhere;
}
.code {
  min-width: 0;
}
.meta {
  color: var(--text-dim);
  font-size: 14px;
  white-space: nowrap;
}
.order {
  white-space: nowrap;
}
.order-detail {
  display: grid;
  gap: 8px;
  overflow-wrap: anywhere;
}
.order-detail h3 {
  margin: 0;
}
.order-detail p {
  margin: 0;
  color: var(--text-dim);
}
@media (max-width: 900px) {
  .row {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }
  .row > :nth-child(n + 4) {
    grid-column: 2 / -1;
    justify-self: start;
  }
}
</style>
