<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NDatePicker,
  NEmpty,
  NInput,
  NModal,
  NSelect,
  NSpin,
  NTag,
  useMessage,
} from 'naive-ui'
import { rewardManagementApi as api } from '@/api/rewards'
import { rewardStatus, rewardSource, rewardError } from '@/lib/rewards'
import type { RewardDefinition } from '@/types'
import RewardUserSearch from './RewardUserSearch.vue'
import type { RewardRecord, RewardRecordDetail } from '@/types/rewards'
const props = defineProps<{ admin?: boolean; userId?: string; definitions?: RewardDefinition[] }>()
const emit = defineEmits<{ changed: [] }>()
const message = useMessage()
const rows = ref<RewardRecord[]>([]),
  cursor = ref(''),
  status = ref(''),
  source = ref('')
const error = ref(''),
  actionError = ref(''),
  loading = ref(false),
  busy = ref(false)
const detail = ref<RewardRecordDetail>(),
  detailOpen = ref(false)
const action = ref<'cancel' | 'reverse' | 'note'>('note'),
  actionOpen = ref(false),
  reason = ref(''),
  target = ref<RewardRecord>()
const asset = ref<string>(),
  filterUser = ref(''),
  dates = ref<[number, number] | null>(null)
const assetOptions = computed(
  () => props.definitions?.map((d) => ({ label: d.name, value: d.id })) || [],
)
let actionRequest = { payload: '', id: '' },
  sequence = 0
const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '待领取', value: 'pending' },
  { label: '已到账', value: 'granted' },
  { label: '已处理', value: 'skipped' },
  { label: '已撤销', value: 'cancelled' },
  { label: '已冲正', value: 'reversed' },
]
const sourceOptions = [
  { label: '全部来源', value: '' },
  { label: '管理员发放', value: 'admin_grant' },
  { label: '挑战结算', value: 'challenge_run' },
  { label: '长期世界终局', value: 'campaign_outcome' },
  { label: '注册赠送', value: 'user_registration' },
  { label: '积分兑换', value: 'reward_exchange' },
]
async function load(more = false) {
  const token = ++sequence
  loading.value = true
  error.value = ''
  try {
    const result = await api.records(!!props.admin, {
      user_id: props.userId || filterUser.value || undefined,
      reward_definition_id: asset.value,
      created_from: dates.value ? new Date(dates.value[0]).toISOString() : undefined,
      created_to: dates.value ? new Date(dates.value[1]).toISOString() : undefined,
      status: status.value,
      source_type: source.value,
      cursor: more ? cursor.value : undefined,
      limit: 30,
    })
    if (token !== sequence) return
    rows.value = more ? [...rows.value, ...result.records] : result.records
    cursor.value = result.next_cursor
  } catch (err) {
    if (token === sequence) error.value = rewardError(err)
  } finally {
    if (token === sequence) loading.value = false
  }
}
async function showDetail(row: RewardRecord) {
  actionError.value = ''
  detail.value = undefined
  detailOpen.value = true
  try {
    detail.value = await api.detail(!!props.admin, row.id)
  } catch (err) {
    actionError.value = rewardError(err)
  }
}
async function claim(row: RewardRecord) {
  busy.value = true
  actionError.value = ''
  try {
    const result = await api.claim(row.id)
    message.success(rewardStatus(result))
    emit('changed')
  } catch (err) {
    try {
      const result = await api.detail(false, row.id)
      if (result.record.status !== 'pending') {
        message.info(rewardStatus(result.record))
        emit('changed')
      } else {
        actionError.value = rewardError(err)
      }
    } catch {
      actionError.value = rewardError(err)
    }
  } finally {
    busy.value = false
    await load()
  }
}
function manage(row: RewardRecord, kind: 'cancel' | 'reverse' | 'note') {
  target.value = row
  action.value = kind
  reason.value = ''
  actionError.value = ''
  actionOpen.value = true
  actionRequest = { payload: '', id: '' }
}
async function submitAction() {
  if (!target.value || !reason.value.trim()) return
  const payload = JSON.stringify([target.value.id, action.value, reason.value])
  if (actionRequest.payload !== payload) actionRequest = { payload, id: crypto.randomUUID() }
  busy.value = true
  actionError.value = ''
  try {
    await api.manage(target.value.id, action.value, reason.value, actionRequest.id)
    actionOpen.value = false
    message.success('操作已保存')
    emit('changed')
    await load()
  } catch (err) {
    actionError.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
watch(
  () => [
    props.userId,
    props.admin,
    status.value,
    source.value,
    asset.value,
    filterUser.value,
    dates.value,
  ],
  () => {
    rows.value = []
    cursor.value = ''
    void load()
  },
  { immediate: true },
)
defineExpose({ reload: () => load() })
</script>
<template>
  <section class="records">
    <div class="toolbar">
      <h3>{{ admin ? '获奖与发放记录' : '奖励记录' }}</h3>
      <NButton :loading="loading" @click="load()">刷新</NButton>
    </div>
    <div v-if="admin && !userId">
      <RewardUserSearch @select="filterUser = $event.id" />
      <div v-if="filterUser">
        <small>筛选用户 {{ filterUser }}</small
        ><NButton text @click="filterUser = ''">清除用户筛选</NButton>
      </div>
    </div>
    <div class="filters">
      <NSelect
        v-if="definitions"
        v-model:value="asset"
        :options="assetOptions"
        clearable
        filterable
        placeholder="全部资产"
      /><NDatePicker v-model:value="dates" type="datetimerange" clearable /><NSelect
        v-model:value="status"
        :options="statusOptions"
        aria-label="奖励状态"
      /><NSelect v-model:value="source" :options="sourceOptions" aria-label="奖励来源" />
    </div>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NAlert v-if="actionError && !actionOpen && !detailOpen" type="error">{{ actionError }}</NAlert>
    <NSpin :show="loading && !rows.length">
      <article v-for="row in rows" :key="row.id" class="record">
        <div>
          <strong
            >{{ row.snapshot.name }} · {{ row.quantity.toLocaleString()
            }}{{ row.snapshot.unit }}</strong
          >
          <p>{{ rewardSource(row) }} · {{ new Date(row.created_at).toLocaleString() }}</p>
          <small v-if="admin && !userId">用户 {{ row.user_id }}</small>
          <p v-if="row.public_note">{{ row.public_note }}</p>
          <NTag :type="row.status === 'pending' ? 'warning' : 'default'" size="small">{{
            rewardStatus(row)
          }}</NTag>
        </div>
        <div class="actions">
          <NButton @click="showDetail(row)">详情</NButton
          ><NButton
            v-if="!admin && row.status === 'pending'"
            type="primary"
            :disabled="busy"
            @click="claim(row)"
            >领取</NButton
          ><template v-if="admin"
            ><NButton
              v-if="row.status === 'pending'"
              :disabled="busy"
              @click="manage(row, 'cancel')"
              >撤销</NButton
            ><NButton
              v-if="
                row.status === 'granted' &&
                row.source_type !== 'reward_exchange' &&
                !row.reversal_ledger_id &&
                row.snapshot.stackable &&
                ['currency', 'credit'].includes(row.snapshot.kind)
              "
              :disabled="busy"
              @click="manage(row, 'reverse')"
              >整笔冲正</NButton
            ><NButton :disabled="busy" @click="manage(row, 'note')">处理说明</NButton></template
          >
        </div>
      </article>
      <NEmpty v-if="!loading && !error && !rows.length" description="没有符合条件的奖励记录" />
    </NSpin>
    <NButton v-if="cursor" :loading="loading" @click="load(true)">加载更多</NButton>
    <NModal
      v-model:show="detailOpen"
      preset="card"
      title="奖励详情"
      style="width: min(680px, 94vw)"
    >
      <NAlert v-if="actionError" type="error">{{ actionError }}</NAlert>
      <div v-if="detail" class="details">
        <h3>
          {{ detail.record.snapshot.name }} · {{ detail.record.quantity
          }}{{ detail.record.snapshot.unit }}
        </h3>
        <p>{{ detail.record.snapshot.description }}</p>
        <p>{{ rewardStatus(detail.record) }} · {{ rewardSource(detail.record) }}</p>
        <p>
          发放方式：{{
            detail.record.delivery_mode === 'claim' ? '用户领取（无期限）' : '直接到账'
          }}
        </p>
        <p>获得时间：{{ new Date(detail.record.created_at).toLocaleString() }}</p>
        <p v-if="detail.record.completed_at">
          处理时间：{{ new Date(detail.record.completed_at).toLocaleString() }}
        </p>
        <p v-if="detail.record.public_note">公开说明：{{ detail.record.public_note }}</p>
        <p v-if="detail.record.cancelled_reason">撤销原因：{{ detail.record.cancelled_reason }}</p>
        <p v-if="detail.record.ledger_id">到账流水：{{ detail.record.ledger_id }}</p>
        <p v-if="detail.record.reversal_ledger_id">
          冲正流水：{{ detail.record.reversal_ledger_id }}
        </p>
        <template v-if="admin"
          ><p>用户：{{ detail.record.user_id }}</p>
          <p>来源：{{ detail.record.source_id }}</p>
          <p>请求：{{ detail.record.request_id || '系统发放' }}</p>
          <p>内部原因：{{ detail.record.reason || '—' }}</p>
          <h4>管理操作</h4>
          <article v-for="audit in detail.audits" :key="audit.id">
            <p>{{ audit.action }} · {{ new Date(audit.created_at).toLocaleString() }}</p>
            <p>操作者：{{ audit.actor_user_id }}</p>
            <p>{{ audit.metadata.reason || '配置或发放操作' }}</p>
          </article></template
        >
      </div>
      <NSpin v-else-if="!actionError" />
    </NModal>
    <NModal
      v-model:show="actionOpen"
      preset="card"
      :mask-closable="!busy"
      :closable="!busy"
      :close-on-esc="!busy"
      :title="
        action === 'reverse'
          ? '确认整笔冲正'
          : action === 'cancel'
            ? '撤销待领取奖励'
            : '添加处理说明'
      "
      style="width: min(560px, 94vw)"
    >
      <p>{{ target?.snapshot.name }} · {{ target?.quantity }}{{ target?.snapshot.unit }}</p>
      <NAlert v-if="action === 'reverse'" type="warning"
        >只扣回该笔账号资产，余额不足时无法冲正。不会恢复局内来源物或重新开放领取。</NAlert
      >
      <p>{{ action === 'cancel' ? '撤销原因（用户可见）' : '内部处理原因' }}</p>
      <NInput v-model:value="reason" type="textarea" :maxlength="2000" :disabled="busy" />
      <NAlert v-if="actionError" type="error">{{ actionError }}</NAlert>
      <template #footer
        ><NButton type="primary" :disabled="!reason.trim()" :loading="busy" @click="submitAction"
          >确认操作</NButton
        ></template
      >
    </NModal>
  </section>
</template>
<style scoped>
.records {
  display: grid;
  gap: 14px;
  margin-top: 24px;
}
.toolbar,
.record {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: center;
}
.toolbar h3 {
  margin: 0;
}
.filters {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.record {
  padding: 16px 0;
  border-bottom: 1px solid var(--border-soft);
}
.record p,
.record small {
  color: var(--text-dim);
  margin: 6px 0;
}
.actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.details {
  overflow-wrap: anywhere;
}
.details article {
  border-top: 1px solid var(--border-soft);
}
@media (max-width: 640px) {
  .record {
    align-items: flex-start;
    flex-direction: column;
  }
  .filters {
    grid-template-columns: 1fr;
  }
}
</style>
