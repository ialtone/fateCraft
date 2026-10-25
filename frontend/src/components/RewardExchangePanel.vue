<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NEmpty, NInputNumber, NModal, NSpin, useMessage } from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import { useAuthStore } from '@/stores/auth'
import { useRewardStore } from '@/stores/rewards'
import { rewardError, POINTS } from '@/lib/rewards'
import type {
  RewardExchangeOffer,
  RewardExchangeOrder,
  RewardExchangeRequest,
} from '@/types/rewards'
import { confirmsPendingExchange, maxExchangeUnits } from '@/lib/rewardExchangeState'
import ExchangeResult from './ExchangeResult.vue'
import PointsCDKRedeem from './PointsCDKRedeem.vue'
const props = defineProps<{ active: boolean }>()
const emit = defineEmits<{ changed: []; pendingChange: [pending: boolean]; viewOrders: [] }>(),
  message = useMessage(),
  rewards = useRewardStore()
const offers = ref<RewardExchangeOffer[]>([]),
  error = ref(''),
  loading = ref(false),
  busy = ref(false),
  open = ref(false),
  selected = ref<RewardExchangeOffer>(),
  units = ref<number | null>(1),
  pending = ref<RewardExchangeRequest>(),
  result = ref<RewardExchangeOrder>(),
  loaded = ref(false)
const auth = useAuthStore()
const storageKey = () => 'fatecraft:reward-exchange:' + auth.user?.id
watch(pending, (value) => {
  emit('pendingChange', !!value)
  try {
    if (value)
      sessionStorage.setItem(
        storageKey(),
        JSON.stringify({ request: value, offer: selected.value }),
      )
    else sessionStorage.removeItem(storageKey())
  } catch {
    /* Result lookup remains available if browser storage is disabled. */
  }
})
const balance = computed(() => rewards.balanceFor(POINTS))
const maxUnits = computed(() =>
  selected.value ? maxExchangeUnits(selected.value, balance.value, !!pending.value) : 1,
)
const valid = computed(
  () =>
    !!selected.value &&
    units.value !== null &&
    Number.isSafeInteger(units.value) &&
    units.value >= 1 &&
    units.value <= maxUnits.value,
)
function soldOut(offer: RewardExchangeOffer) {
  return !offer.stock_remaining
}
// 不可兑换的原因；空字符串表示至少可兑换 1 份。
function blockReason(offer: RewardExchangeOffer) {
  if (soldOut(offer)) return '已售罄'
  if (offer.remaining_units === 0) return '已达兑换上限'
  if (balance.value < offer.cost_quantity)
    return `积分不足：需 ${offer.cost_quantity}，当前 ${balance.value}`
  return ''
}
function limitText(offer: RewardExchangeOffer) {
  if (!offer.max_redemptions_per_user) return ''
  const redeemed = offer.redeemed_units ?? 0
  const remaining = offer.remaining_units ?? Math.max(0, offer.max_redemptions_per_user - redeemed)
  return `您可兑换 ${remaining} / ${offer.max_redemptions_per_user}`
}
function stockText(offer: RewardExchangeOffer) {
  return `商品剩余 ${offer.stock_remaining ?? 0} / ${offer.stock_total ?? 0}`
}
let loadPromise: Promise<boolean> | undefined
function load(): Promise<boolean> {
  if (loadPromise) return loadPromise
  loading.value = true
  error.value = ''
  loadPromise = (async () => {
    try {
      offers.value = await api.offers()
      // Unresolved requests retain their original confirmation snapshot.
      if (selected.value && !pending.value)
        selected.value = offers.value.find((offer) => offer.id === selected.value?.id)
      loaded.value = true
      return true
    } catch (err) {
      error.value = rewardError(err)
      return false
    } finally {
      loading.value = false
      loadPromise = undefined
    }
  })()
  return loadPromise
}
async function refreshAfterExchange() {
  // An already-running read may precede the committed order.
  if (loadPromise) await loadPromise
  await load()
}
function choose(offer: RewardExchangeOffer) {
  if (blockReason(offer)) return
  if (pending.value) {
    open.value = true
    return
  }
  selected.value = offer
  units.value = 1
  pending.value = undefined
  result.value = undefined
  error.value = ''
  open.value = true
}
async function completed(order: RewardExchangeOrder) {
  if (!confirmsPendingExchange(pending.value, order)) {
    throw new Error('原请求结果与本次兑换内容不一致，请检查兑换记录。')
  }
  result.value = order
  pending.value = undefined
  message.success('兑换成功')
  emit('changed')
  void refreshAfterExchange()
}
async function submit() {
  if (busy.value || !selected.value || !valid.value) return
  if (!pending.value) {
    pending.value = {
      request_id: crypto.randomUUID(),
      offer_id: selected.value.id,
      revision: selected.value.revision,
      units: units.value!,
    }
  }
  busy.value = true
  error.value = ''
  try {
    await completed(await api.exchange(pending.value))
  } catch (err) {
    try {
      await completed(await api.request(pending.value.request_id))
    } catch {
      error.value = rewardError(err)
      const status = (err as { response?: { status: number } }).response?.status
      const code = (err as { response?: { data?: { code?: string } } }).response?.data?.code
      if (status && [400, 401, 403, 404, 409, 422].includes(status)) pending.value = undefined
      if (code === 'exchange_stock_insufficient') {
        await refreshPrice()
        error.value = '库存不足，请刷新后重试'
      }
      if (code === 'exchange_limit_reached') {
        const refreshed = await refreshPrice()
        error.value = refreshed
          ? '本次兑换份数超过剩余额度，请根据最新额度重新确认。'
          : '本次兑换份数超过剩余额度，刷新失败：' + error.value
      }
    }
  } finally {
    busy.value = false
  }
}
async function queryPending() {
  if (!pending.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await completed(await api.request(pending.value.request_id))
  } catch (err) {
    error.value = '暂未确认兑换结果，请保留原请求并稍后查询或重试同次兑换。' + rewardError(err)
  } finally {
    busy.value = false
  }
}
function resumePending() {
  if (pending.value) open.value = true
}
function acceptConfirmedOrder(order: RewardExchangeOrder) {
  if (!confirmsPendingExchange(pending.value, order)) return
  result.value = order
  pending.value = undefined
  error.value = ''
  void refreshAfterExchange()
}
function viewOrders() {
  open.value = false
  emit('viewOrders')
}
defineExpose({ resumePending, acceptConfirmedOrder })
watch(
  () => props.active,
  (active) => {
    if (active && !loaded.value) void load()
    if (!active && !busy.value) open.value = false
  },
  { immediate: true },
)
async function refreshPrice() {
  if (pending.value) return false
  if (!(await load())) return false
  units.value = 1
  return true
}
onMounted(() => {
  try {
    const saved = JSON.parse(sessionStorage.getItem(storageKey()) || 'null')
    if (saved?.request?.request_id && saved?.offer?.id) {
      selected.value = saved.offer
      pending.value = saved.request
      units.value = saved.request.units
    }
  } catch {
    /* Ignore malformed local state. */
  }
})
</script>
<template>
  <section class="exchange">
    <div class="toolbar">
      <div>
        <h2>积分兑换</h2>
        <p>通关积累积分，兑换站内奖励或 CDK。</p>
      </div>
      <NButton :loading="loading" @click="load">刷新商品</NButton>
    </div>
    <PointsCDKRedeem @redeemed="emit('changed')" />
    <NAlert v-if="error && !open" type="error">{{ error }}</NAlert
    ><NSpin :show="loading"
      ><div class="offer-grid">
        <article v-for="offer in offers" :key="offer.id">
          <h3>{{ offer.name }}</h3>
          <p>{{ offer.description }}</p>
          <strong
            >{{ offer.cost_quantity }} 积分{{
              offer.kind === 'reward' ? ` → ${offer.reward_quantity} 次剧情改写` : ' / 份'
            }}</strong
          >
          <NButton type="primary" :disabled="!!blockReason(offer)" @click="choose(offer)">{{
            soldOut(offer) ? '已售罄' : blockReason(offer) ? '不可兑换' : '兑换'
          }}</NButton>
          <small v-if="blockReason(offer) && !soldOut(offer)" class="reason">{{
            blockReason(offer)
          }}</small>
          <footer>
            <span>{{ limitText(offer) }}</span
            ><span>{{ stockText(offer) }}</span>
          </footer>
        </article>
      </div>
      <NEmpty v-if="!loading && !error && !offers.length" description="暂无可兑换商品"
    /></NSpin>
    <NModal
      v-model:show="open"
      preset="card"
      :title="result ? '兑换成功' : pending ? '确认兑换结果' : '确认兑换'"
      :mask-closable="!busy"
      :closable="!busy"
      :close-on-esc="!busy"
      style="width: min(620px, 94vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
    >
      <NAlert v-if="error" type="error" class="modal-error">{{ error }}</NAlert>
      <template v-if="result"
        ><NAlert type="success"
          >{{ result.snapshot.name }} × {{ result.units
          }}<template v-if="result.snapshot.kind !== 'cdk'"
            >；获得 {{ result.reward_quantity }} 次剧情改写</template
          ></NAlert
        >
        <ExchangeResult :order="result" />
        <p class="identifier">订单 {{ result.id }}</p>
        <div class="toolbar">
          <NButton type="primary" @click="viewOrders">查看兑换记录</NButton>
        </div></template
      >
      <template v-else-if="selected"
        ><div class="confirm">
          <div>
            <h3>{{ selected.name }}</h3>
            <p>{{ selected.description }}</p>
          </div>
          <label class="units"
            ><span>兑换份数</span
            ><NInputNumber
              v-model:value="units"
              :disabled="busy || !!pending || maxUnits === 0"
              :min="1"
              :max="Math.max(1, maxUnits)"
              :precision="0"
              update-value-on-input
              style="width: 160px"
            /><small>{{
              maxUnits > 0 ? `最多 ${maxUnits} 份` : blockReason(selected) || '当前不可兑换'
            }}</small></label
          >
          <dl>
            <dt>单价</dt>
            <dd>{{ selected.cost_quantity }} 积分 / 份</dd>
            <dt>支付</dt>
            <dd>
              {{ valid ? selected.cost_quantity * (units ?? 1) : '—' }} 积分<template
                v-if="selected.kind === 'reward'"
                >，获得
                {{ valid ? selected.reward_quantity * (units ?? 1) : '—' }} 次剧情改写</template
              >
            </dd>
            <dt>当前余额</dt>
            <dd>{{ balance }} 积分</dd>
            <dt>兑换后</dt>
            <dd>{{ valid ? balance - selected.cost_quantity * (units ?? 1) : '—' }} 积分</dd>
            <dt>额度</dt>
            <dd>{{ limitText(selected) || '每人兑换不限' }} · {{ stockText(selected) }}</dd>
          </dl>
        </div>
        <NAlert v-if="pending" type="warning"
          >结果尚未确认，请保留请求 ID 并重试同次兑换。
          <p class="identifier">{{ pending.request_id }}</p></NAlert
        >
        <div class="toolbar">
          <NButton v-if="pending" :loading="busy" @click="queryPending">查询本次结果</NButton
          ><NButton type="primary" :loading="busy" :disabled="!valid" @click="submit">{{
            pending ? '重试同次兑换' : '确认扣除并兑换'
          }}</NButton>
        </div></template
      >
      <NEmpty v-else description="商品已下架，请重新选择" />
    </NModal>
  </section>
</template>
<style scoped>
.exchange {
  display: grid;
  gap: 20px;
  padding: 20px;
  border: 1px solid var(--border-soft);
  border-radius: 12px;
}
.toolbar {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
}
.offer-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 14px;
}
article {
  display: grid;
  gap: 12px;
  padding: 16px;
  background: var(--bg-soft);
  border-radius: 8px;
}
h2,
h3,
p {
  margin: 0;
}
p {
  color: var(--text-dim);
}
footer {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  color: var(--text-dim);
  font-size: 13px;
}
footer span:last-child {
  margin-left: auto;
}
.modal-error {
  margin-bottom: 12px;
}
/* 默认禁用态透明度过低，提高可读性。 */
article :deep(.n-button--disabled) {
  opacity: 0.8;
}
.reason {
  color: var(--error, #c2410c);
}
.confirm {
  display: grid;
  gap: 16px;
  margin-bottom: 16px;
}
.confirm > div {
  display: grid;
  gap: 6px;
}
.units {
  display: flex;
  align-items: center;
  gap: 12px;
}
.units small {
  color: var(--text-dim);
}
dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 8px 16px;
  margin: 0;
  padding: 12px 16px;
  background: var(--bg-soft);
  border-radius: 8px;
}
dt {
  color: var(--text-dim);
}
dd {
  margin: 0;
}
.identifier {
  overflow-wrap: anywhere;
  margin: 12px 0;
}
@media (max-width: 600px) {
  .toolbar {
    align-items: start;
    flex-direction: column;
  }
}
</style>
