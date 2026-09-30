<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NEmpty, NSpin, NTabs, NTabPane } from 'naive-ui'
import { useRewardStore } from '@/stores/rewards'
import RewardRecordsPanel from '@/components/RewardRecordsPanel.vue'
import RewardExchangePanel from '@/components/RewardExchangePanel.vue'
import RewardLedgerPanel from '@/components/RewardLedgerPanel.vue'
import RewardExchangeOrders from '@/components/RewardExchangeOrders.vue'
import type { RewardExchangeOrder } from '@/types/rewards'
import { rewardError } from '@/lib/rewards'
const rewards = useRewardStore()
const tab = ref('exchange')
const pendingExchange = ref(false)
const exchange = ref<InstanceType<typeof RewardExchangePanel>>()
const error = ref(''),
  revision = ref(0)
const records = ref<InstanceType<typeof RewardRecordsPanel>>()
const balances = computed(() =>
  rewards.items.filter((r) => ['currency', 'credit'].includes(r.definition.kind)),
)
const collections = computed(() =>
  rewards.items.filter((r) => !['currency', 'credit'].includes(r.definition.kind)),
)
async function load() {
  error.value = ''
  try {
    await rewards.load()
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    // Order/ledger refresh must not depend on the balance endpoint succeeding.
    revision.value++
  }
}
async function refresh() {
  await load()
  await records.value?.reload()
}
function resumeExchange() {
  tab.value = 'exchange'
  exchange.value?.resumePending()
}
async function confirmedOrder(order: RewardExchangeOrder) {
  exchange.value?.acceptConfirmedOrder(order)
  await refresh()
}
onMounted(load)
</script>
<template>
  <div class="reward-page">
    <header class="page-head">
      <div>
        <p class="eyebrow">PLAYER REWARDS</p>
        <h1>我的奖励</h1>
        <p>通关获得奖励，积累点数，兑换账号奖励或额度。</p>
      </div>
      <NButton :loading="rewards.loading" @click="refresh">刷新奖励</NButton>
    </header>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NAlert v-if="pendingExchange" type="warning" class="pending-exchange">
      <div class="pending-content">
        <span>有一笔兑换结果尚未确认，可查询结果或重试同次兑换。</span
        ><NButton size="small" @click="resumeExchange">继续确认</NButton>
      </div>
    </NAlert>
    <NTabs v-model:value="tab" type="line" animated class="reward-tabs">
      <NTabPane name="exchange" tab="积分兑换" display-directive="show"
        ><RewardExchangePanel
          ref="exchange"
          :active="tab === 'exchange'"
          @changed="refresh"
          @pending-change="pendingExchange = $event"
          @view-orders="tab = 'orders'"
      /></NTabPane>
      <NTabPane name="overview" tab="资产概览" display-directive="show:lazy"
        ><div class="overview-panels">
          <NSpin :show="rewards.loading && !rewards.loaded">
            <section
              v-for="group in [
                { title: '余额与点数 / 额度', items: balances },
                { title: '藏品与成就', items: collections },
              ]"
              :key="group.title"
              class="reward-section"
            >
              <h2>{{ group.title }}</h2>
              <div v-if="group.items.length" class="reward-grid">
                <article v-for="item in group.items" :key="item.definition.id" class="reward-card">
                  <div class="reward-copy">
                    <strong>{{ item.definition.name }}</strong>
                    <p>{{ item.definition.description }}</p>
                  </div>
                  <div class="reward-balance">
                    {{ item.balance.toLocaleString() }}<small>{{ item.definition.unit }}</small>
                  </div>
                </article>
              </div>
              <NEmpty v-else-if="!error && !rewards.loading" description="暂无可用资产" />
            </section>
          </NSpin></div
      ></NTabPane>
      <NTabPane name="records" tab="奖励记录" display-directive="show:lazy"
        ><section class="reward-section">
          <p class="tab-hint">领取待领取奖励，查看通关、赠送和兑换获得的奖励。</p>
          <RewardRecordsPanel ref="records" @changed="load" /></section
      ></NTabPane>
      <NTabPane name="orders" tab="兑换记录" display-directive="show:lazy"
        ><section class="reward-section">
          <RewardExchangeOrders :refresh-key="revision" @confirmed="confirmedOrder" /></section
      ></NTabPane>
      <NTabPane name="ledger" tab="余额流水" display-directive="show:lazy"
        ><section class="reward-section">
          <p class="tab-hint">查看奖励到账、消费和返还带来的余额变化。</p>
          <RewardLedgerPanel :refresh-key="revision" /></section
      ></NTabPane>
    </NTabs>
  </div>
</template>
<style scoped>
.reward-tabs {
  min-width: 0;
}
.overview-panels :deep(.n-spin-content) {
  display: grid;
  gap: 18px;
}
.tab-hint {
  margin: 0 0 16px;
  color: var(--text-dim);
  font-size: 13px;
}
.pending-content {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}
.pending-content > span {
  min-width: 0;
}
.pending-content :deep(button) {
  flex-shrink: 0;
}

.reward-page {
  width: min(1040px, 100%);
  margin: 0 auto;
  display: grid;
  gap: 22px;
}

.page-head,
.section-head,
.reward-title,
.ledger-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.page-head h1,
.reward-section h2 {
  margin: 0;
}

.page-head p:not(.eyebrow) {
  margin: 8px 0 0;
  color: var(--text-dim);
}

.eyebrow {
  margin: 0 0 4px;
  color: var(--accent);
  font-size: 11px;
  letter-spacing: 2px;
}

.reward-section {
  padding: 20px;
  border: 1px solid var(--border-soft);
  border-radius: 12px;
  background: var(--bg-soft);
}

.reward-section h2 {
  margin-bottom: 14px;
  font-size: 17px;
}

.section-head h2 {
  margin-bottom: 0;
}

.section-head span,
.reward-key,
.ledger-main span,
.ledger-meta time {
  color: var(--text-faint);
  font-size: 12px;
}

.reward-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.reward-card {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 13px;
  align-items: center;
  min-width: 0;
  padding: 15px;
  border: 1px solid var(--border-soft);
  border-radius: 10px;
  background: var(--bg);
}

.reward-card.collectible {
  border-color: color-mix(in srgb, var(--accent) 28%, var(--border-soft));
}

.reward-icon {
  width: 36px;
  height: 36px;
  padding: 8px;
  color: var(--accent);
  border-radius: 50%;
  background: var(--accent-soft);
}

.reward-copy {
  min-width: 0;
}

.reward-title {
  justify-content: flex-start;
  gap: 8px;
}

.reward-copy p {
  margin: 5px 0;
  color: var(--text-dim);
  font-size: 13px;
}

.reward-key {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.reward-balance {
  color: var(--text);
  font-size: 24px;
  font-weight: 700;
  line-height: 1;
}

.reward-balance small {
  margin-left: 4px;
  color: var(--text-dim);
  font-size: 12px;
  font-weight: 400;
  line-height: 1;
}

.ledger-list {
  margin-top: 14px;
  border-top: 1px solid var(--border-soft);
}

.ledger-row {
  padding: 13px 2px;
  border-bottom: 1px solid var(--border-soft);
}

.ledger-main,
.ledger-meta {
  display: grid;
  gap: 3px;
}

.ledger-meta {
  text-align: right;
}

.ledger-meta strong {
  color: var(--text-dim);
}

.ledger-meta strong.positive {
  color: var(--success, #18a058);
}

@media (max-width: 720px) {
  .pending-content {
    align-items: flex-start;
    flex-direction: column;
  }
  .page-head {
    align-items: flex-start;
  }

  .reward-grid {
    grid-template-columns: 1fr;
  }

  .reward-section {
    padding: 15px;
  }

  .reward-card {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .reward-balance {
    font-size: 20px;
  }

  .ledger-row {
    align-items: flex-start;
  }
}
</style>
