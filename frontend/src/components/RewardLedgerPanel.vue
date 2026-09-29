<script setup lang="ts">
import { ref, watch } from 'vue'
import { NAlert, NButton, NEmpty, NSpin } from 'naive-ui'
import { rewardManagementApi } from '@/api/rewards'
import { platformValue } from '@/lib/presentation'
import { rewardError } from '@/lib/rewards'
import type { RewardLedgerEntry } from '@/types'
const props = defineProps<{ userId?: string; refreshKey?: number }>()
const rows = ref<RewardLedgerEntry[]>([]),
  cursor = ref(''),
  error = ref(''),
  loading = ref(false)
let sequence = 0
async function load(more = false) {
  const token = ++sequence
  error.value = ''
  loading.value = true
  try {
    const result = await rewardManagementApi.ledger(props.userId, more ? cursor.value : undefined)
    if (token !== sequence) return
    rows.value = more ? [...rows.value, ...result.entries] : result.entries
    cursor.value = result.next_cursor
  } catch (err) {
    if (token === sequence) error.value = rewardError(err)
  } finally {
    if (token === sequence) loading.value = false
  }
}
watch(
  () => [props.userId, props.refreshKey],
  () => {
    rows.value = []
    void load()
  },
  { immediate: true },
)
</script>
<template>
  <section class="ledger">
    <div class="heading">
      <h3>余额流水</h3>
      <NButton :loading="loading" @click="load()">刷新</NButton>
    </div>
    <NAlert v-if="error" type="error">{{ error }}</NAlert
    ><NSpin :show="loading && !rows.length"
      ><article v-for="row in rows" :key="row.id">
        <div>
          <strong
            >{{ row.definition.name }} · {{ row.quantity_delta > 0 ? '+' : ''
            }}{{ row.quantity_delta.toLocaleString() }}{{ row.definition.unit }}</strong
          >
          <p>
            {{ platformValue('reward_operation', row.operation) }} ·
            {{ platformValue('reward_source', row.source_type) }} ·
            {{ new Date(row.created_at).toLocaleString() }}
          </p>
          <small
            >{{ row.id
            }}<template v-if="row.related_entry_id">
              · 关联原流水 {{ row.related_entry_id }}</template
            ></small
          >
        </div>
      </article>
      <NEmpty v-if="!loading && !error && !rows.length" description="暂无余额流水" /></NSpin
    ><NButton v-if="cursor" :loading="loading" @click="load(true)">加载更多</NButton>
  </section>
</template>
<style scoped>
.ledger {
  display: grid;
  gap: 12px;
  margin-top: 24px;
}
.heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.ledger article {
  padding: 12px 0;
  border-bottom: 1px solid var(--border-soft);
}
.ledger p,
.ledger small {
  color: var(--text-dim);
  overflow-wrap: anywhere;
}
</style>
