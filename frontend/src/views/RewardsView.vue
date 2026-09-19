<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { GiftOutline, RefreshOutline } from '@vicons/ionicons5'
import { NButton, NEmpty, NIcon, NSpin, NTag, useMessage } from 'naive-ui'
import { rewardsApi } from '@/api'
import { useRewardStore } from '@/stores/rewards'
import { platformValue } from '@/lib/presentation'
import type { RewardLedgerEntry } from '@/types'

const message = useMessage()
const rewards = useRewardStore()
const ledger = ref<RewardLedgerEntry[]>([])
const loadingLedger = ref(false)

const collections = computed(() =>
  rewards.items.filter((item) => ['badge', 'entitlement', 'item'].includes(item.definition.kind)),
)
const balances = computed(() =>
  rewards.items.filter((item) => ['currency', 'credit'].includes(item.definition.kind)),
)

function kindLabel(kind: string) {
  return platformValue('reward_kind', kind, '其他奖励')
}

function operationLabel(operation: RewardLedgerEntry['operation']) {
  return platformValue('reward_operation', operation)
}

function sourceLabel(source: string) {
  return platformValue('reward_source', source, '其他来源')
}

function signedQuantity(entry: RewardLedgerEntry) {
  const quantity = entry.quantity_delta
  return `${quantity > 0 ? '+' : ''}${quantity.toLocaleString()}${entry.definition.unit || ''}`
}

function formatTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

async function load() {
  loadingLedger.value = true
  try {
    const [, entries] = await Promise.all([rewards.load(), rewardsApi.ledger(100)])
    ledger.value = entries
  } catch (err: any) {
    message.error(`读取奖励失败：${err.message || '未知错误'}`)
  } finally {
    loadingLedger.value = false
  }
}

onMounted(() => void load())
</script>

<template>
  <div class="reward-page">
    <header class="page-head">
      <div>
        <p class="eyebrow">PLAYER REWARDS</p>
        <h1>我的奖励</h1>
        <p>奖励由挑战结算和平台功能产生，余额与流水均以后端账本为准。</p>
      </div>
      <NButton secondary :loading="rewards.loading || loadingLedger" @click="load">
        <template #icon
          ><NIcon><RefreshOutline /></NIcon
        ></template>
        刷新
      </NButton>
    </header>

    <NSpin :show="rewards.loading && !rewards.loaded">
      <section class="reward-section">
        <h2>余额与额度</h2>
        <div v-if="balances.length" class="reward-grid">
          <article v-for="item in balances" :key="item.definition.key" class="reward-card">
            <NIcon class="reward-icon"><GiftOutline /></NIcon>
            <div class="reward-copy">
              <div class="reward-title">
                <strong>{{ item.definition.name }}</strong>
                <NTag size="small" :bordered="false">{{ kindLabel(item.definition.kind) }}</NTag>
              </div>
              <p>{{ item.definition.description || '暂无说明' }}</p>
            </div>
            <div class="reward-balance">
              {{ item.balance.toLocaleString() }}<small>{{ item.definition.unit }}</small>
            </div>
          </article>
        </div>
        <NEmpty v-else description="暂时没有可用余额" />
      </section>

      <section class="reward-section">
        <h2>藏品与成就</h2>
        <div v-if="collections.length" class="reward-grid">
          <article
            v-for="item in collections"
            :key="item.definition.key"
            class="reward-card collectible"
          >
            <NIcon class="reward-icon"><GiftOutline /></NIcon>
            <div class="reward-copy">
              <div class="reward-title">
                <strong>{{ item.definition.name }}</strong>
                <NTag size="small" type="success" :bordered="false">{{
                  kindLabel(item.definition.kind)
                }}</NTag>
              </div>
              <p>{{ item.definition.description || '暂无说明' }}</p>
            </div>
            <div class="reward-balance">
              {{ item.balance.toLocaleString() }}<small>{{ item.definition.unit }}</small>
            </div>
          </article>
        </div>
        <NEmpty v-else description="完成挑战后，徽章和藏品会出现在这里" />
      </section>
    </NSpin>

    <section class="reward-section">
      <div class="section-head">
        <h2>奖励流水</h2>
        <span>最近 {{ ledger.length }} 条</span>
      </div>
      <NSpin :show="loadingLedger">
        <div v-if="ledger.length" class="ledger-list">
          <article v-for="entry in ledger" :key="entry.id" class="ledger-row">
            <div class="ledger-main">
              <strong>{{ entry.definition.name }}</strong>
              <span
                >{{ operationLabel(entry.operation) }} · {{ sourceLabel(entry.source_type) }}</span
              >
            </div>
            <div class="ledger-meta">
              <strong :class="{ positive: entry.quantity_delta > 0 }">{{
                signedQuantity(entry)
              }}</strong>
              <time :datetime="entry.created_at">{{ formatTime(entry.created_at) }}</time>
            </div>
          </article>
        </div>
        <NEmpty v-else description="还没有奖励流水" />
      </NSpin>
    </section>
  </div>
</template>

<style scoped>
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
  grid-template-columns: auto minmax(0, 1fr) auto;
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
    grid-template-columns: auto minmax(0, 1fr);
  }

  .reward-balance {
    grid-column: 2;
    font-size: 20px;
  }

  .ledger-row {
    align-items: flex-start;
  }
}
</style>
