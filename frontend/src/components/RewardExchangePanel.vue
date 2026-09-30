<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NEmpty, NInputNumber, NModal, NSpin, useMessage } from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import { useAuthStore } from '@/stores/auth'
import { useRewardStore } from '@/stores/rewards'
import { rewardError } from '@/lib/rewards'
import type {
  RewardExchangeOfferView,
  RewardExchangeOrder,
  RewardExchangeRequest,
} from '@/types/rewards'
import { confirmsPendingExchange } from '@/lib/rewardExchangeState'
const props = defineProps<{ active: boolean }>()
const emit = defineEmits<{ changed: []; pendingChange: [pending: boolean]; viewOrders: [] }>(),
  message = useMessage(),
  rewards = useRewardStore()
const offers = ref<RewardExchangeOfferView[]>([]),
  error = ref(''),
  loading = ref(false),
  busy = ref(false),
  open = ref(false),
  selected = ref<RewardExchangeOfferView>(),
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
const maxUnits = computed(() =>
  selected.value?.reward_asset.stackable
    ? Math.min(
        Math.floor(Number.MAX_SAFE_INTEGER / selected.value.cost_quantity),
        Math.floor(Number.MAX_SAFE_INTEGER / selected.value.reward_quantity),
      )
    : 1,
)
const valid = computed(
  () =>
    !!selected.value &&
    units.value !== null &&
    Number.isSafeInteger(units.value) &&
    units.value >= 1 &&
    units.value <= maxUnits.value,
)
const balance = computed(
  () =>
    rewards.items.find((r) => r.definition.id === selected.value?.cost_definition_id)?.balance ?? 0,
)
async function load() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    offers.value = await api.offers()
    loaded.value = true
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    loading.value = false
  }
}
function choose(offer: RewardExchangeOfferView) {
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
  result.value = order
  pending.value = undefined
  message.success('兑换完成，奖励已到账')
  emit('changed')
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
      if (status && [400, 401, 403, 404, 409, 422].includes(status)) pending.value = undefined
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
  if (pending.value || loading.value) return
  const id = selected.value?.id
  await load()
  selected.value = offers.value.find((o) => o.id === id)
  units.value = 1
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
        <p>通关积累的点数可兑换账号奖励和额度。</p>
      </div>
      <NButton :loading="loading" @click="load">刷新商品</NButton>
    </div>
    <NAlert v-if="error && !open" type="error">{{ error }}</NAlert
    ><NSpin :show="loading"
      ><div class="offer-grid">
        <article v-for="offer in offers" :key="offer.id">
          <h3>{{ offer.name }}</h3>
          <p>{{ offer.description }}</p>
          <strong
            >{{ offer.cost_quantity }} {{ offer.cost_asset.name }} → {{ offer.reward_quantity }}
            {{ offer.reward_asset.name }}</strong
          ><NButton type="primary" @click="choose(offer)">兑换</NButton>
        </article>
      </div>
      <NEmpty v-if="!loading && !error && !offers.length" description="暂无可兑换商品"
    /></NSpin>
    <NModal
      v-model:show="open"
      preset="card"
      :title="result ? '兑换完成' : pending ? '确认兑换结果' : '确认兑换'"
      :mask-closable="!busy"
      :closable="!busy"
      :close-on-esc="!busy"
      style="width: min(620px, 94vw)"
    >
      <NAlert v-if="error" type="error">{{ error }}</NAlert>
      <template v-if="result"
        ><NAlert type="success"
          >兑换已完成：{{ result.snapshot.name }} × {{ result.units }}；获得
          {{ result.reward_quantity }} {{ result.snapshot.reward_asset.name }}</NAlert
        >
        <p class="identifier">订单 {{ result.id }}</p>
        <div class="toolbar">
          <NButton @click="open = false">继续浏览商品</NButton>
          <NButton type="primary" @click="viewOrders">查看兑换记录</NButton>
        </div></template
      >
      <template v-else-if="selected"
        ><h3>{{ selected.name }}</h3>
        <p>{{ selected.description }}</p>
        <p>当前余额：{{ balance }} {{ selected.cost_asset.name }}</p>
        <NInputNumber
          v-model:value="units"
          :disabled="busy || !!pending"
          :min="1"
          :max="maxUnits"
          :precision="0"
          aria-label="兑换份数"
        />
        <p>
          支付 {{ valid ? selected.cost_quantity * (units ?? 1) : '—' }}
          {{ selected.cost_asset.name }}，获得
          {{ valid ? selected.reward_quantity * (units ?? 1) : '—' }}
          {{ selected.reward_asset.name }}
        </p>
        <p v-if="!selected.reward_asset.stackable">已经拥有该奖励时无法兑换，积分不会扣除。</p>
        <NAlert v-if="pending" type="warning"
          >结果尚未确认，请保留请求 ID 并重试同次兑换。
          <p class="identifier">{{ pending.request_id }}</p></NAlert
        >
        <div class="toolbar">
          <NButton v-if="pending" :loading="busy" @click="queryPending">查询本次结果</NButton
          ><NButton v-else :disabled="busy || loading" @click="refreshPrice">刷新价格</NButton
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
