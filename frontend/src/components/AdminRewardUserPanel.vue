<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NTabs,
  NTabPane,
} from 'naive-ui'
import { rewardManagementApi as api } from '@/api/rewards'
import { useAuthStore } from '@/stores/auth'
import { POINTS, rewardError, rewardName, rewardOptions } from '@/lib/rewards'
import type { RewardGrantRequest, RewardKey, RewardUserBalance } from '@/types/rewards'
import type { RewardLedgerEntry } from '@/types'
import RewardLedgerPanel from './RewardLedgerPanel.vue'
const props = defineProps<{ userId: string }>(),
  auth = useAuthStore()
const data = ref<RewardUserBalance>(),
  error = ref(''),
  tab = ref('grant'),
  revision = ref(0),
  key = ref<RewardKey>(POINTS),
  quantity = ref<number | null>(100),
  reason = ref(''),
  note = ref(''),
  pending = ref<RewardGrantRequest>(),
  open = ref(false),
  busy = ref(false),
  actionError = ref(''),
  result = ref<RewardLedgerEntry>()
const storageKey = 'fatecraft:admin-grant:' + auth.user?.id + ':' + props.userId
const valid = computed(
  () =>
    !!data.value &&
    data.value.user.status === 'active' &&
    quantity.value !== null &&
    Number.isSafeInteger(quantity.value) &&
    quantity.value > 0 &&
    quantity.value <= Number.MAX_SAFE_INTEGER &&
    !!reason.value.trim(),
)
async function load() {
  error.value = ''
  try {
    data.value = await api.user(props.userId)
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    revision.value++
  }
}
function confirm() {
  if (!valid.value) return
  result.value = undefined
  actionError.value = ''
  open.value = true
}
function viewLedger() {
  open.value = false
  tab.value = 'ledger'
}
function persist() {
  try {
    if (pending.value) sessionStorage.setItem(storageKey, JSON.stringify(pending.value))
    else sessionStorage.removeItem(storageKey)
  } catch {
    /* 查询入口仍可使用原请求 ID。 */
  }
}
async function completed(entry: RewardLedgerEntry) {
  const request = pending.value
  if (
    !request ||
    entry.user_id !== props.userId ||
    entry.request_id !== request.request_id ||
    entry.operation !== 'grant' ||
    entry.source_type !== 'admin_grant' ||
    entry.reward_key !== request.reward_key ||
    entry.quantity_delta !== request.quantity ||
    entry.reason !== request.reason ||
    entry.public_note !== request.public_note
  ) {
    throw new Error('原请求结果与本次发放内容不一致，请检查收支记录。')
  }
  result.value = entry
  pending.value = undefined
  persist()
  await load()
}
async function send() {
  if (busy.value || (!pending.value && !valid.value)) return
  pending.value ||= {
    request_id: crypto.randomUUID(),
    reward_key: key.value,
    quantity: quantity.value!,
    reason: reason.value,
    public_note: note.value,
  }
  persist()
  busy.value = true
  actionError.value = ''
  try {
    await completed(await api.grant(props.userId, pending.value))
  } catch (err) {
    try {
      await completed(await api.request(pending.value.request_id))
    } catch {
      actionError.value = rewardError(err)
      const status = (err as { response?: { status: number } }).response?.status
      if (status && [400, 403, 404, 409].includes(status)) {
        pending.value = undefined
        persist()
      }
    }
  } finally {
    busy.value = false
  }
}
async function query() {
  if (!pending.value || busy.value) return
  busy.value = true
  actionError.value = ''
  try {
    await completed(await api.request(pending.value.request_id))
  } catch (err) {
    actionError.value = '暂未确认，请保留原请求。' + rewardError(err)
  } finally {
    busy.value = false
  }
}
onMounted(() => {
  void load()
  try {
    const saved = JSON.parse(sessionStorage.getItem(storageKey) || 'null')
    if (
      saved?.request_id &&
      rewardOptions.some((o) => o.value === saved.reward_key) &&
      Number.isSafeInteger(saved.quantity)
    ) {
      pending.value = saved
      key.value = saved.reward_key
      quantity.value = saved.quantity
      reason.value = saved.reason
      note.value = saved.public_note
    }
  } catch {
    /* 忽略无效本地数据。 */
  }
})
</script>
<template>
  <div class="user-rewards">
    <NAlert v-if="error" type="error"
      >{{ error }} <NButton size="small" @click="load">重试</NButton></NAlert
    >
    <template v-if="data"
      ><h3>
        {{ data.user.display_name || data.user.username }} <small>{{ data.user.id }}</small>
      </h3>
      <div class="balances">
        <span v-for="item in data.rewards" :key="item.asset.key"
          >{{ item.asset.name }} <strong>{{ item.balance.toLocaleString() }}</strong></span
        >
      </div>
      <NAlert v-if="pending" type="warning"
        >有一笔发放尚未确认。<NButton size="small" @click="open = true">继续确认</NButton></NAlert
      >
      <NTabs v-model:value="tab" type="line"
        ><NTabPane name="grant" tab="发放" display-directive="show:lazy"
          ><NFormItem label="发放项目"
            ><NSelect
              v-model:value="key"
              :options="rewardOptions"
              :disabled="!!pending" /></NFormItem
          ><NFormItem label="数量"
            ><NInputNumber
              v-model:value="quantity"
              :disabled="!!pending"
              :min="1"
              :max="Number.MAX_SAFE_INTEGER"
              :precision="0" /></NFormItem
          ><NFormItem label="内部原因（必填）"
            ><NInput
              v-model:value="reason"
              :disabled="!!pending"
              type="textarea"
              :maxlength="2000" /></NFormItem
          ><NFormItem label="给用户的说明（可选）"
            ><NInput v-model:value="note" :disabled="!!pending" :maxlength="2000" /></NFormItem
          ><NButton type="primary" :disabled="!valid || !!pending" @click="confirm"
            >确认发放</NButton
          ></NTabPane
        >
        <NTabPane name="ledger" tab="收支" display-directive="show:lazy"
          ><RewardLedgerPanel
            :user-id="userId"
            :refresh-key="revision"
            @changed="load" /></NTabPane></NTabs
    ></template>
    <NModal
      v-model:show="open"
      preset="card"
      title="确认管理员发放"
      :mask-closable="!busy"
      :closable="!busy"
      :close-on-esc="!busy"
      style="width: min(560px, 94vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
      ><NAlert v-if="actionError" type="error">{{ actionError }}</NAlert
      ><NAlert v-if="result" :type="result.reversal_id ? 'warning' : 'success'"
        ><template v-if="result.reversal_id"
          >这笔发放已完成，随后已被撤回。请查看收支记录。</template
        >
        <template v-else
          >已发放 {{ result.quantity_delta }} {{ rewardName(result.reward_key) }}，变动后余额
          {{ result.balance_after }}。</template
        ></NAlert
      ><template v-else
        ><p>目标：{{ data?.user.display_name || data?.user.username }}（{{ userId }}）</p>
        <p>发放 {{ pending?.quantity ?? quantity }} {{ rewardName(pending?.reward_key ?? key) }}</p>
        <p>原因：{{ pending?.reason ?? reason }}</p>
        <p v-if="pending" class="id">请求 {{ pending.request_id }}</p>
        <div class="buttons">
          <NButton v-if="pending" :loading="busy" @click="query">查询本次结果</NButton
          ><NButton type="primary" :loading="busy" @click="send">{{
            pending ? '重试同次发放' : '确认到账'
          }}</NButton>
        </div></template
      ><NButton v-if="result" @click="viewLedger">查看收支</NButton></NModal
    >
  </div>
</template>
<style scoped>
.user-rewards {
  display: grid;
  gap: 16px;
}
h3 {
  margin: 0;
}
small {
  display: block;
  font-size: 12px;
  color: var(--text-faint);
  overflow-wrap: anywhere;
}
.balances,
.buttons {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
}
.balances span {
  padding: 14px 18px;
  border-radius: 10px;
  background: var(--bg-soft);
}
strong {
  margin-left: 12px;
}
.id {
  overflow-wrap: anywhere;
}
p {
  color: var(--text-dim);
}
/* 结果提示与“查看收支”按钮相邻时留出间距 */
.n-alert + .n-button {
  margin-top: 14px;
}
</style>
