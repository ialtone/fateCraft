<script setup lang="ts">
import { ref, watch } from 'vue'
import { NAlert, NButton, NDatePicker, NEmpty, NInput, NModal, NSelect, NSpin } from 'naive-ui'
import { rewardManagementApi as api } from '@/api/rewards'
import { rewardError, rewardName, rewardOptions, rewardSource } from '@/lib/rewards'
import type { RewardLedgerEntry } from '@/types'
import type { RewardKey, RewardFilters } from '@/types/rewards'
const props = defineProps<{ admin?: boolean; userId?: string; refreshKey?: number }>()
const emit = defineEmits<{ changed: [] }>()
const rows = ref<RewardLedgerEntry[]>([]),
  cursor = ref(''),
  error = ref(''),
  loading = ref(false),
  key = ref<RewardKey | null>(null),
  source = ref<string | null>(null),
  dates = ref<[number, number] | null>(null),
  user = ref(''),
  detail = ref<RewardLedgerEntry>(),
  open = ref(false),
  reason = ref(''),
  request = ref(''),
  busy = ref(false),
  actionError = ref('')
const sources = [
  'challenge_run',
  'campaign_outcome',
  'campaign_stage',
  'user_registration',
  'admin_grant',
  'admin_reversal',
  'reward_exchange',
  'message_rewrite',
  'message_regenerate',
  'message_rewrite_refund',
].map((value) => ({ label: rewardSource(value), value }))
let sequence = 0
let appliedFilters: RewardFilters = {}
async function load(more = false) {
  const token = ++sequence
  loading.value = true
  error.value = ''
  try {
    const filters = more
      ? appliedFilters
      : {
          user_id: props.userId || user.value.trim() || undefined,
          reward_key: key.value || undefined,
          source_type: source.value || undefined,
          created_from: dates.value ? new Date(dates.value[0]).toISOString() : undefined,
          created_to: dates.value ? new Date(dates.value[1]).toISOString() : undefined,
        }
    const page = await api.ledger(!!props.admin || !!props.userId, {
      ...filters,
      cursor: more ? cursor.value : undefined,
    })
    if (token !== sequence) return
    appliedFilters = filters
    rows.value = more ? [...rows.value, ...page.entries] : page.entries
    cursor.value = page.next_cursor
  } catch (err) {
    if (token === sequence) error.value = rewardError(err)
  } finally {
    if (token === sequence) loading.value = false
  }
}
function show(row: RewardLedgerEntry) {
  detail.value = row
  reason.value = ''
  request.value = ''
  actionError.value = ''
  open.value = true
}
async function reverse() {
  if (!detail.value || busy.value || !reason.value.trim()) return
  request.value ||= crypto.randomUUID()
  busy.value = true
  actionError.value = ''
  try {
    await api.reverse(detail.value.id, reason.value, request.value)
    open.value = false
    emit('changed')
    await load()
  } catch (err) {
    try {
      const entry = await api.request(request.value)
      if (entry.related_entry_id !== detail.value.id || entry.operation !== 'reverse') throw err
      open.value = false
      emit('changed')
      await load()
    } catch {
      actionError.value = rewardError(err)
      const status = (err as { response?: { status: number } }).response?.status
      if (status && [400, 403, 404, 409].includes(status)) request.value = ''
    }
  } finally {
    busy.value = false
  }
}
watch(
  () => [props.userId, props.refreshKey],
  () => load(),
  { immediate: true },
)
</script>
<template>
  <section class="ledger">
    <div class="toolbar">
      <h3>收支记录</h3>
      <NButton :loading="loading" @click="load()">刷新</NButton>
    </div>
    <form class="filters" @submit.prevent="load()">
      <NSelect
        v-model:value="key"
        :options="rewardOptions"
        clearable
        placeholder="全部项目"
      /><NSelect
        v-model:value="source"
        :options="sources"
        clearable
        placeholder="全部来源"
      /><NDatePicker v-model:value="dates" type="datetimerange" clearable /><NInput
        v-if="admin && !userId"
        v-model:value="user"
        placeholder="用户 ID（留空查看全部）"
      /><NButton attr-type="submit">筛选</NButton>
    </form>
    <NAlert v-if="error" type="error">{{ error }}</NAlert
    ><NSpin :show="loading && !rows.length">
      <article v-for="row in rows" :key="row.id">
        <div>
          <strong
            >{{ rewardName(row.reward_key) }} {{ row.quantity_delta > 0 ? '+' : ''
            }}{{ row.quantity_delta.toLocaleString() }}</strong
          >
          <p>
            {{ rewardSource(row.source_type) }}<span v-if="row.reversal_id"> · 已撤回</span> ·
            变动后 {{ row.balance_after.toLocaleString() }}
          </p>
          <small>{{ new Date(row.created_at).toLocaleString() }}</small>
          <p v-if="row.public_note">{{ row.public_note }}</p>
        </div>
        <NButton v-if="admin || userId" @click="show(row)">详情</NButton>
      </article>
      <NEmpty v-if="!loading && !error && !rows.length" description="暂无收支记录" /></NSpin
    ><NButton v-if="cursor" :loading="loading" @click="load(true)">加载更多</NButton>
    <NModal
      v-model:show="open"
      preset="card"
      title="收支详情"
      :mask-closable="!busy && !request"
      :closable="!busy && !request"
      :close-on-esc="!busy && !request"
      style="width: min(640px, 94vw)"
    >
      <template v-if="detail"
        ><p>用户：{{ detail.user_id }}</p>
        <p>
          {{ rewardName(detail.reward_key) }}：{{ detail.quantity_delta }}；变动后：{{
            detail.balance_after
          }}
        </p>
        <p>来源：{{ rewardSource(detail.source_type) }}</p>
        <p v-if="detail.operator_id">操作者：{{ detail.operator_id }}</p>
        <p v-if="detail.reason">内部原因：{{ detail.reason }}</p>
        <p class="id">流水：{{ detail.id }}</p>
        <template v-if="detail.source_type === 'admin_grant' && !detail.reversal_id"
          ><NInput
            v-model:value="reason"
            :disabled="!!request || busy"
            type="textarea"
            :maxlength="2000"
            placeholder="填写撤回原因；将整笔扣回本次发放，余额不足时不会执行。"
          /><NAlert v-if="actionError" type="error">{{ actionError }}</NAlert>
          <p v-if="request" class="id">结果尚未确认，请保留此请求并重试：{{ request }}</p>
          <NButton type="warning" :disabled="!reason.trim()" :loading="busy" @click="reverse">{{
            request ? '重试同次撤回' : '确认撤回本次发放'
          }}</NButton></template
        >
      </template>
    </NModal>
  </section>
</template>
<style scoped>
.ledger {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.toolbar,
article {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
.filters {
  display: grid;
  grid-template-columns: 160px 180px minmax(240px, 1fr) auto;
  gap: 12px;
}
article {
  padding: 16px 0;
  border-bottom: 1px solid var(--border-soft);
}
h3,
p {
  margin: 0;
}
p {
  margin-top: 7px;
  color: var(--text-dim);
}
small {
  color: var(--text-faint);
}
.id {
  overflow-wrap: anywhere;
  margin-bottom: 12px;
}
@media (max-width: 900px) {
  .filters {
    grid-template-columns: 1fr 1fr;
  }
}
@media (max-width: 600px) {
  .filters {
    grid-template-columns: 1fr;
  }
  article {
    align-items: flex-start;
  }
}
</style>
