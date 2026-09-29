<script setup lang="ts">
import { ref, watch } from 'vue'
import { NAlert, NButton, NEmpty, NInput, NModal, NSpin } from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import type { RewardExchangeOrder } from '@/types/rewards'
import { rewardError } from '@/lib/rewards'
const props = defineProps<{ admin?: boolean; refreshKey?: number }>()
const rows = ref<RewardExchangeOrder[]>([]),
  cursor = ref(''),
  userID = ref(''),
  error = ref(''),
  loading = ref(false),
  detail = ref<RewardExchangeOrder>(),
  open = ref(false)
let sequence = 0
async function load(more = false) {
  const token = ++sequence
  loading.value = true
  error.value = ''
  try {
    const result = await api.orders(
      !!props.admin,
      more ? cursor.value : undefined,
      userID.value.trim() || undefined,
    )
    if (token !== sequence) return
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
function showOrder(row: RewardExchangeOrder) {
  detail.value = row
  open.value = true
}
</script>
<template>
  <section class="orders">
    <div class="toolbar">
      <h3>兑换订单</h3>
      <NButton :loading="loading" @click="load()">刷新</NButton>
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
            支付 {{ row.cost_quantity }} {{ row.snapshot.cost_asset.name }} → 获得
            {{ row.reward_quantity }} {{ row.snapshot.reward_asset.name }}
          </p>
          <small
            >{{ new Date(row.created_at).toLocaleString() }} · 已完成<span v-if="admin">
              · 用户 {{ row.user_id }}</span
            ></small
          >
        </div>
        <NButton @click="showOrder(row)">详情</NButton>
      </article>
      <NEmpty v-if="!loading && !error && !rows.length" description="暂无兑换订单" /></NSpin
    ><NButton v-if="cursor" :loading="loading" @click="load(true)">加载更多</NButton>
    <NModal v-model:show="open" preset="card" title="兑换详情" style="width: min(640px, 94vw)"
      ><div v-if="detail" class="detail">
        <h3>{{ detail.snapshot.name }} × {{ detail.units }}</h3>
        <p>{{ detail.snapshot.description }}</p>
        <p>
          支付 {{ detail.cost_quantity }} {{ detail.snapshot.cost_asset.name }}；获得
          {{ detail.reward_quantity }} {{ detail.snapshot.reward_asset.name }}
        </p>
        <p>商品修订：{{ detail.offer_revision }}</p>
        <p>订单：{{ detail.id }}</p>
        <p>请求：{{ detail.request_id }}</p>
        <p>支付流水：{{ detail.cost_ledger_id }}</p>
        <p>获奖记录：{{ detail.reward_record_id }}</p>
      </div></NModal
    >
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
article {
  border-bottom: 1px solid var(--border-soft);
  padding: 14px 0;
}
p,
small {
  color: var(--text-dim);
}
.detail {
  overflow-wrap: anywhere;
}
h3 {
  margin: 0;
}
@media (max-width: 600px) {
  article {
    align-items: start;
    flex-direction: column;
  }
}
</style>
