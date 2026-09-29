<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NEmpty,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  useMessage,
} from 'naive-ui'
import { rewardManagementApi as api } from '@/api/rewards'
import type { AdminUser, RewardDefinition, AdminAuditLog } from '@/types'
import type {
  RegistrationRewardRule,
  RewardRecord,
  RewardAssetOverview,
  RewardRuleCatalogEntry,
} from '@/types/rewards'
import { rewardError, rewardStatus } from '@/lib/rewards'
import RewardUserSearch from './RewardUserSearch.vue'
import AdminRewardUserPanel from './AdminRewardUserPanel.vue'
import RewardRecordsPanel from './RewardRecordsPanel.vue'
import AdminRewardExchangePanel from './AdminRewardExchangePanel.vue'
const props = defineProps<{ definitions: RewardDefinition[] }>(),
  emit = defineEmits<{ openVersion: [id: string] }>(),
  message = useMessage()
const selected = ref<AdminUser>(),
  userOpen = ref(false),
  error = ref(''),
  saving = ref(false),
  tab = ref('overview')
const rule = ref<RegistrationRewardRule>(),
  requestID = ref(''),
  found = ref<RewardRecord>(),
  assets = ref<RewardAssetOverview[]>([]),
  rules = ref<RewardRuleCatalogEntry[]>([]),
  ruleAsset = ref<string>(),
  ruleOffset = ref(0),
  ruleMore = ref(false),
  audits = ref<AdminAuditLog[]>([]),
  auditCursor = ref(''),
  busy = ref(false)
const options = computed(() =>
  props.definitions.map((d) => ({
    label: `${d.name} · ${d.key}${d.disabled ? '（已停用）' : ''}`,
    value: d.id,
  })),
)
const registrationOptions = computed(() =>
  options.value.map((o) => ({
    ...o,
    disabled: props.definitions.find((d) => d.id === o.value)?.disabled,
  })),
)
function select(user: AdminUser) {
  selected.value = user
  userOpen.value = true
}
async function overview() {
  error.value = ''
  busy.value = true
  try {
    assets.value = await api.overview()
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
async function loadRules(more = false) {
  busy.value = true
  error.value = ''
  try {
    const offset = more ? ruleOffset.value + 30 : 0
    const page = await api.rules(ruleAsset.value, offset)
    rules.value = more ? [...rules.value, ...page.rules] : page.rules
    ruleOffset.value = offset
    ruleMore.value = page.has_more
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
async function loadRegistration() {
  try {
    rule.value = await api.registration()
  } catch (err) {
    error.value = rewardError(err)
  }
}
async function save() {
  if (!rule.value) return
  saving.value = true
  error.value = ''
  try {
    rule.value = await api.saveRegistration(rule.value)
    message.success('规则已保存，新注册用户按新规则执行')
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    saving.value = false
  }
}
async function lookup() {
  error.value = ''
  found.value = undefined
  try {
    found.value = await api.request(requestID.value.trim())
  } catch (err) {
    error.value = rewardError(err)
  }
}
async function loadAudits(more = false) {
  busy.value = true
  error.value = ''
  try {
    const page = await api.audits(more ? auditCursor.value : undefined)
    audits.value = more ? [...audits.value, ...page.audits] : page.audits
    auditCursor.value = page.next_cursor
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
function auditLabel(action: string) {
  return (
    (
      {
        reward_grant: '发放奖励',
        reward_cancel: '撤销待领取',
        reward_reverse: '整笔冲正',
        reward_note: '处理说明',
        reward_definition_create: '创建奖励资产',
        reward_definition_update: '编辑奖励资产',
        reward_registration_rule: '调整注册赠送',
        reward_registration_disable: '停用注册赠送',
        reward_builtin_import: '导入玩法奖励',
        reward_exchange_offer: '调整兑换商品',
      } as Record<string, string>
    )[action] || action
  )
}
watch(tab, (value) => {
  error.value = ''
  if (value === 'overview') void overview()
  if (value === 'rules') {
    void loadRules()
    void loadRegistration()
  }
  if (value === 'records') void loadAudits()
})
watch(ruleAsset, () => loadRules())
onMounted(overview)
</script>
<template>
  <div class="admin-rewards">
    <p class="intro">通关发奖、积累点数、兑换账号奖励。这里统一管理资产、规则与每笔发放。</p>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NTabs v-model:value="tab" type="line" animated>
      <NTabPane name="overview" tab="概览"
        ><div class="toolbar">
          <h3>按资产统计</h3>
          <NButton :loading="busy" @click="overview">刷新</NButton>
        </div>
        <p>各资产独立核算；待领取不计入可用余额。累计授予包含兑换所得，累计消费包含兑换支出。</p>
        <div class="overview-grid">
          <article v-for="asset in assets" :key="asset.id" class="asset-card">
            <div class="toolbar">
              <h3>{{ asset.name }}</h3>
              <NTag v-if="asset.disabled" type="warning" size="small">停用</NTag>
            </div>
            <small>{{ asset.key }} · {{ asset.unit }}</small>
            <dl>
              <dt>当前总余额</dt>
              <dd>{{ asset.balance }}</dd>
              <dt>累计授予 / 消费</dt>
              <dd>{{ asset.granted }} / {{ asset.consumed }}</dd>
              <dt>累计返还 / 冲正</dt>
              <dd>{{ asset.refunded }} / {{ asset.reversed }}</dd>
              <dt>待领取数量</dt>
              <dd>{{ asset.pending }}</dd>
              <dt>兑换支出 / 所得</dt>
              <dd>{{ asset.exchange_cost }} / {{ asset.exchange_reward }}</dd>
            </dl>
          </article>
        </div>
        <NEmpty v-if="!busy && !error && !assets.length" description="暂无奖励资产"
      /></NTabPane>
      <NTabPane name="assets" tab="奖励资产"><slot name="assets" /></NTabPane>
      <NTabPane name="rules" tab="发奖规则"
        ><h3>玩法发奖规则</h3>
        <p>规则随玩法版本发布；已发布版本和已有存档保持固定。点击版本可查看，编辑时创建草稿。</p>
        <NSelect
          v-model:value="ruleAsset"
          :options="options"
          clearable
          filterable
          placeholder="按奖励资产筛选"
        />
        <article v-for="item in rules" :key="item.id" class="rule-row">
          <div>
            <strong>{{ item.pack_name }} · {{ item.version }}</strong>
            <p>
              {{ item.reward_name }} · {{ item.calculator_key }} ·
              {{ item.enabled ? '启用' : '停用' }}
            </p>
            <small
              >{{
                item.current
                  ? '当前入口版本'
                  : item.version_status === 'published'
                    ? '历史发布版本'
                    : '草稿'
              }}
              · {{ JSON.stringify(item.calculator_config) }}</small
            >
          </div>
          <NButton @click="emit('openVersion', item.challenge_version_id)">查看 / 编辑版本</NButton>
        </article>
        <NEmpty v-if="!busy && !error && !rules.length" description="暂无玩法发奖规则" /><NButton
          v-if="ruleMore"
          :loading="busy"
          @click="loadRules(true)"
          >加载更多规则</NButton
        >
        <section v-if="rule">
          <h3>注册赠送</h3>
          <p>保存后影响新注册账号，已有赠送记录保留原规则。</p>
          <div class="rule-form">
            <NFormItem label="启用"><NSwitch v-model:value="rule.enabled" /></NFormItem
            ><NFormItem label="赠送资产"
              ><NSelect
                v-model:value="rule.reward_definition_id"
                :options="registrationOptions"
                filterable /></NFormItem
            ><NFormItem label="数量"
              ><NInputNumber
                v-model:value="rule.quantity"
                :min="1"
                :max="Number.MAX_SAFE_INTEGER"
                :precision="0"
            /></NFormItem>
          </div>
          <NButton :loading="saving" @click="save">保存注册赠送规则</NButton
          ><small>当前修订 {{ rule.id }} · {{ new Date(rule.created_at).toLocaleString() }}</small>
        </section></NTabPane
      >
      <NTabPane name="users" tab="用户发放"
        ><h3>查找用户，发放奖励与点数</h3>
        <p>支持用户名、显示名称、完整邮箱或用户 ID。选定账号后可直接发放或创建待领取奖励。</p>
        <RewardUserSearch @select="select"
      /></NTabPane>
      <NTabPane name="exchange" tab="积分兑换"
        ><AdminRewardExchangePanel :definitions="definitions"
      /></NTabPane>
      <NTabPane name="records" tab="记录与审计"
        ><section>
          <h3>查询原发放请求</h3>
          <div class="request">
            <NInput v-model:value="requestID" placeholder="粘贴发放请求 ID" /><NButton
              :disabled="!requestID.trim()"
              @click="lookup"
              >查询</NButton
            >
          </div>
          <NAlert v-if="found" type="info"
            >{{ found.snapshot.name }} · {{ found.quantity }}{{ found.snapshot.unit }} ·
            {{ rewardStatus(found) }} · 用户 {{ found.user_id }}</NAlert
          >
        </section>
        <RewardRecordsPanel admin :definitions="definitions" />
        <section>
          <div class="toolbar">
            <h3>管理审计</h3>
            <NButton :loading="busy" @click="loadAudits()">刷新</NButton>
          </div>
          <details v-for="audit in audits" :key="audit.id">
            <summary>
              {{ auditLabel(audit.action) }} · {{ new Date(audit.created_at).toLocaleString() }}
            </summary>
            <p>
              操作者 {{ audit.actor_user_id
              }}<span v-if="audit.target_user_id"> · 目标用户 {{ audit.target_user_id }}</span>
            </p>
            <p>请求 {{ audit.request_id }}</p>
            <pre>{{ JSON.stringify(audit.metadata, null, 2) }}</pre>
          </details>
          <NEmpty v-if="!busy && !error && !audits.length" description="暂无奖励管理操作" /><NButton
            v-if="auditCursor"
            :loading="busy"
            @click="loadAudits(true)"
            >加载更多审计</NButton
          >
        </section></NTabPane
      >
    </NTabs>
    <NModal
      v-model:show="userOpen"
      preset="card"
      title="用户奖励与点数"
      style="width: min(1040px, 96vw)"
      ><AdminRewardUserPanel v-if="selected" :key="selected.id" :user-id="selected.id"
    /></NModal>
  </div>
</template>
<style scoped>
.admin-rewards {
  display: grid;
  gap: 16px;
}
.intro,
p,
small {
  color: var(--text-dim);
}
.toolbar,
.rule-row {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: center;
}
h3 {
  margin: 0;
}
.overview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 16px;
}
.asset-card {
  padding: 18px;
  border: 1px solid var(--border-soft);
  border-radius: 10px;
  background: var(--bg-soft);
}
dl {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 10px;
  font-size: 13px;
}
dd {
  margin: 0;
  text-align: right;
  overflow-wrap: anywhere;
}
dt {
  color: var(--text-dim);
}
.rule-row {
  padding: 16px 0;
  border-bottom: 1px solid var(--border-soft);
}
section {
  margin-top: 28px;
  padding-top: 20px;
  border-top: 1px solid var(--border-soft);
}
small {
  display: block;
  margin-top: 8px;
}
.rule-form {
  display: grid;
  grid-template-columns: 80px 1fr 180px;
  gap: 14px;
}
.request {
  display: flex;
  gap: 12px;
  margin: 12px 0;
}
details {
  padding: 12px 0;
  overflow-wrap: anywhere;
}
summary {
  cursor: pointer;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 12px;
}
@media (max-width: 640px) {
  .rule-form {
    grid-template-columns: 1fr;
  }
  .rule-row {
    align-items: start;
    flex-direction: column;
  }
}
</style>
