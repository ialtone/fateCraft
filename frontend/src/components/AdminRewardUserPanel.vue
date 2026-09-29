<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpin,
  useMessage,
} from 'naive-ui'
import { adminApi } from '@/api'
import { rewardManagementApi as api } from '@/api/rewards'
import type { RewardDefinition } from '@/types'
import type { RewardUserBalance, RewardGrantRequest, RewardRecord } from '@/types/rewards'
import { rewardError, rewardStatus } from '@/lib/rewards'
import RewardRecordsPanel from './RewardRecordsPanel.vue'
import RewardLedgerPanel from './RewardLedgerPanel.vue'
const props = defineProps<{ userId: string }>()
const message = useMessage()
const data = ref<RewardUserBalance>(),
  definitions = ref<RewardDefinition[]>([]),
  loading = ref(false),
  busy = ref(false),
  error = ref(''),
  operationError = ref('')
const records = ref<InstanceType<typeof RewardRecordsPanel>>(),
  revision = ref(0)
const form = ref<Omit<RewardGrantRequest, 'request_id'>>({
  reward_definition_id: '',
  quantity: 1,
  purpose: 'gift',
  delivery_mode: 'direct',
  reason: '',
  public_note: '',
})
const prepared = ref<{
  userId: string
  username: string
  definition: RewardDefinition
  balance: number
  body: RewardGrantRequest
}>()
const preview = ref(false),
  result = ref<RewardRecord>()
const selected = computed(() =>
  definitions.value.find((d) => d.id === form.value.reward_definition_id),
)
const options = computed(() =>
  definitions.value
    .filter((d) => !d.disabled)
    .map((d) => ({ label: `${d.name}（${d.unit || d.kind}） · ${d.key}`, value: d.id })),
)
const balance = computed(
  () => data.value?.rewards.find((r) => r.definition.id === selected.value?.id)?.balance || 0,
)
const valid = computed(
  () =>
    !!selected.value &&
    data.value?.user.status === 'active' &&
    Number.isSafeInteger(form.value.quantity) &&
    form.value.quantity > 0 &&
    !!form.value.reason.trim(),
)
let sequence = 0
async function load() {
  const token = ++sequence
  loading.value = true
  error.value = ''
  try {
    const [user, defs] = await Promise.all([
      api.user(props.userId),
      adminApi.listRewardDefinitions(),
    ])
    if (token !== sequence) return
    data.value = user
    definitions.value = defs
    revision.value++
  } catch (err) {
    if (token === sequence) error.value = rewardError(err)
  } finally {
    if (token === sequence) loading.value = false
  }
}
async function refresh() {
  await load()
  await records.value?.reload()
}
function confirm() {
  if (!valid.value || !selected.value || !data.value) return
  if (prepared.value && !result.value) {
    const { request_id: _requestID, ...previous } = prepared.value.body
    if (
      prepared.value.userId === props.userId &&
      JSON.stringify(previous) === JSON.stringify(form.value)
    ) {
      preview.value = true
      return
    }
  }
  prepared.value = {
    userId: props.userId,
    username: data.value.user.username,
    definition: { ...selected.value },
    balance: balance.value,
    body: { ...form.value, request_id: crypto.randomUUID() },
  }
  result.value = undefined
  operationError.value = ''
  preview.value = true
}
async function finish(row: RewardRecord) {
  result.value = row
  message.success(rewardStatus(row))
  await refresh()
}
async function submit() {
  if (!prepared.value) return
  busy.value = true
  operationError.value = ''
  try {
    await finish(await api.grant(prepared.value.userId, prepared.value.body))
  } catch (err) {
    try {
      await finish(await api.request(prepared.value.body.request_id))
    } catch {
      operationError.value = `${rewardError(err)}。可查询原请求或重试同次发放。`
    }
  } finally {
    busy.value = false
  }
}
async function queryResult() {
  if (!prepared.value) return
  busy.value = true
  operationError.value = ''
  try {
    await finish(await api.request(prepared.value.body.request_id))
  } catch (err) {
    operationError.value = `${rewardError(err)}。暂未查到时可重试同一请求，不能据此认定一定未发放。`
  } finally {
    busy.value = false
  }
}
watch(
  () => props.userId,
  () => {
    data.value = undefined
    result.value = undefined
    prepared.value = undefined
    preview.value = false
    void load()
  },
  { immediate: true },
)
watch(selected, (def) => {
  if (def && !def.stackable) form.value.quantity = 1
})
</script>
<template>
  <div class="user-rewards">
    <NAlert v-if="error" type="error">{{ error }}</NAlert
    ><NSpin :show="loading && !data"
      ><template v-if="data">
        <h3>{{ data.user.display_name }} · @{{ data.user.username }}</h3>
        <p class="muted">{{ data.user.email || '无邮箱' }} · {{ data.user.id }}</p>
        <NAlert v-if="data.user.status !== 'active'" type="warning"
          >该账号已停用，可以查看记录，不能新发放奖励。</NAlert
        >
        <div class="balances">
          <div v-for="item in data.rewards" :key="item.definition.id">
            <strong>{{ item.definition.name }}</strong>
            <p>{{ item.balance.toLocaleString() }}{{ item.definition.unit }}</p>
          </div>
          <p v-if="!data.rewards.length">尚无奖励余额，仍可发放奖励。</p>
        </div>
        <h3>发放奖励或点数</h3>
        <div class="form-grid">
          <NFormItem label="奖励或点数资产"
            ><NSelect
              v-model:value="form.reward_definition_id"
              :options="options"
              filterable
              placeholder="选择具体资产"
          /></NFormItem>
          <NFormItem :label="`数量${selected?.unit ? `（${selected.unit}）` : ''}`"
            ><NInputNumber
              v-model:value="form.quantity"
              :min="1"
              :max="selected && !selected.stackable ? 1 : Number.MAX_SAFE_INTEGER"
              :precision="0"
              style="width: 100%"
          /></NFormItem>
          <NFormItem label="用途"
            ><NSelect
              v-model:value="form.purpose"
              :options="[
                { label: '赠送', value: 'gift' },
                { label: '补偿', value: 'compensation' },
              ]"
          /></NFormItem>
          <NFormItem label="发放方式"
            ><NSelect
              v-model:value="form.delivery_mode"
              :options="[
                { label: '直接到账', value: 'direct' },
                { label: '用户领取（无期限）', value: 'claim' },
              ]"
          /></NFormItem>
        </div>
        <NFormItem label="管理原因（必填，仅管理员可见）"
          ><NInput v-model:value="form.reason" type="textarea" :maxlength="2000" /></NFormItem
        ><NFormItem label="给用户的说明（可选）"
          ><NInput v-model:value="form.public_note" :maxlength="2000"
        /></NFormItem>
        <NButton type="primary" :disabled="!valid || busy" @click="confirm">预览发放</NButton>
        <RewardRecordsPanel
          ref="records"
          :definitions="definitions"
          admin
          :user-id="userId"
          @changed="load"
        /><RewardLedgerPanel :user-id="userId" :refresh-key="revision" /> </template
    ></NSpin>
    <NModal
      v-model:show="preview"
      preset="card"
      title="确认发放奖励或点数"
      :mask-closable="!busy"
      :closable="!busy"
      :close-on-esc="!busy"
      style="width: min(600px, 94vw)"
    >
      <template v-if="prepared"
        ><p>账号：@{{ prepared.username }}</p>
        <p class="muted">{{ prepared.userId }}</p>
        <h3>
          {{ prepared.definition.name }} · {{ prepared.body.quantity
          }}{{ prepared.definition.unit }}
        </h3>
        <p>当前余额：{{ prepared.balance.toLocaleString() }}{{ prepared.definition.unit }}</p>
        <p>用途：{{ prepared.body.purpose === 'gift' ? '赠送' : '补偿' }}</p>
        <NAlert v-if="prepared.body.delivery_mode === 'claim'" type="info"
          >创建待领取奖励，当前余额不变，用户领取时到账。</NAlert
        ><NAlert v-else-if="!prepared.definition.stackable && prepared.balance > 0" type="warning"
          >该用户已拥有此资产，将不会重复发放；实际结果以提交时状态为准。</NAlert
        >
        <p v-else>
          直接到账，预计增加 {{ prepared.body.quantity }}{{ prepared.definition.unit }}。
        </p>
        <p>管理原因：{{ prepared.body.reason }}</p>
        <p v-if="prepared.body.public_note">公开说明：{{ prepared.body.public_note }}</p>
        <small class="muted">发放请求：{{ prepared.body.request_id }}</small>
        <NAlert v-if="operationError" type="error">{{ operationError }}</NAlert
        ><NAlert v-if="result" type="success"
          >{{ rewardStatus(result)
          }}<template v-if="result.status === 'granted'">
            · 本次增加 {{ result.quantity }}{{ result.snapshot.unit }} · 处理后余额
            {{ result.balance_after }}</template
          ><template v-else-if="result.status === 'pending'">，当前余额未增加。</template></NAlert
        >
      </template>
      <template #footer
        ><div class="buttons">
          <NButton v-if="!result" :loading="busy" @click="queryResult">查询原请求</NButton
          ><NButton v-if="!result" type="primary" :loading="busy" @click="submit">{{
            operationError ? '重试同次发放' : '确认发放'
          }}</NButton
          ><NButton v-else @click="preview = false">完成</NButton>
        </div></template
      >
    </NModal>
  </div>
</template>
<style scoped>
.user-rewards {
  display: grid;
  gap: 12px;
}
.muted {
  color: var(--text-dim);
  overflow-wrap: anywhere;
}
.balances {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  margin: 20px 0;
}
.balances > div {
  padding: 14px;
  background: var(--bg-soft);
  border: 1px solid var(--border-soft);
  border-radius: 10px;
  min-width: 130px;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.buttons {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
@media (max-width: 640px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
