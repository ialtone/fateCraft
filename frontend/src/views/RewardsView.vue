<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NAlert, NButton, NTabs, NTabPane } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'
import { useRewardStore } from '@/stores/rewards'
import RewardExchangePanel from '@/components/RewardExchangePanel.vue'
import RewardLedgerPanel from '@/components/RewardLedgerPanel.vue'
import RewardExchangeOrders from '@/components/RewardExchangeOrders.vue'
import type { RewardExchangeOrder } from '@/types/rewards'
import { rewardError } from '@/lib/rewards'
const rewards = useRewardStore(),
  auth = useAuthStore()
const tab = ref('exchange'),
  pendingExchange = ref(false),
  error = ref(''),
  revision = ref(0)
const exchange = ref<InstanceType<typeof RewardExchangePanel>>()
async function refresh() {
  error.value = ''
  try {
    await rewards.load()
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    revision.value++
  }
}
function resumeExchange() {
  tab.value = 'exchange'
  exchange.value?.resumePending()
}
async function confirmedOrder(order: RewardExchangeOrder) {
  exchange.value?.acceptConfirmedOrder(order)
  await refresh()
}
onMounted(refresh)
</script>
<template>
  <div class="reward-page">
    <header>
      <div>
        <p class="eyebrow">PLAYER REWARDS</p>
        <h1>我的奖励</h1>
        <p>通关赚积分，兑换剧情改写次数。</p>
      </div>
      <NButton :loading="rewards.loading" @click="refresh">刷新</NButton>
    </header>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <div class="balances">
      <article v-for="item in rewards.items" :key="item.asset.key">
        <span>{{ item.asset.name }}</span
        ><strong
          >{{ item.balance.toLocaleString() }}<small>{{ item.asset.unit }}</small></strong
        >
      </article>
    </div>
    <NAlert v-if="pendingExchange" type="warning"
      ><div class="pending">
        <span>有一笔兑换结果尚未确认。</span
        ><NButton size="small" @click="resumeExchange">继续确认</NButton>
      </div></NAlert
    >
    <NTabs v-model:value="tab" type="line" animated>
      <NTabPane name="exchange" tab="积分兑换" display-directive="show"
        ><RewardExchangePanel
          :key="auth.user?.id"
          ref="exchange"
          :active="tab === 'exchange'"
          @changed="refresh"
          @pending-change="pendingExchange = $event"
          @view-orders="tab = 'orders'"
      /></NTabPane>
      <NTabPane name="orders" tab="兑换记录" display-directive="show:lazy"
        ><RewardExchangeOrders :refresh-key="revision" @confirmed="confirmedOrder"
      /></NTabPane>
      <NTabPane name="ledger" tab="收支记录" display-directive="show:lazy"
        ><RewardLedgerPanel :refresh-key="revision"
      /></NTabPane>
    </NTabs>
  </div>
</template>
<style scoped>
.reward-page {
  width: min(1040px, 100%);
  margin: 0 auto;
  display: grid;
  gap: 22px;
  min-width: 0;
}
header,
.pending {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 14px;
}
h1 {
  margin: 0;
}
header p {
  color: var(--text-dim);
  margin: 8px 0 0;
}
.eyebrow {
  font-size: 11px;
  letter-spacing: 2px;
  color: var(--accent);
}
.balances {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.balances article {
  padding: 20px;
  border: 1px solid var(--border-soft);
  border-radius: 12px;
  background: var(--bg-soft);
  display: grid;
  gap: 12px;
}
.balances span {
  color: var(--text-dim);
}
strong {
  font-size: 28px;
}
small {
  font-size: 12px;
  margin-left: 8px;
  font-weight: 400;
}
@media (max-width: 600px) {
  header {
    align-items: flex-start;
  }
  .balances {
    gap: 10px;
  }
  .balances article {
    padding: 14px;
  }
  strong {
    font-size: 24px;
  }
}
</style>
