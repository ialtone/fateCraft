<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  NAlert,
  NButton,
  NEmpty,
  NFormItem,
  NInputNumber,
  NModal,
  NSpin,
  NTabs,
  NTabPane,
} from 'naive-ui'
import { rewardManagementApi as api } from '@/api/rewards'
import { rewardError } from '@/lib/rewards'
import type { AdminUser, AdminAuditLog } from '@/types'
import type { RegistrationRewardRule, RewardRuleCatalogEntry } from '@/types/rewards'
import RewardUserSearch from './RewardUserSearch.vue'
import AdminRewardUserPanel from './AdminRewardUserPanel.vue'
import AdminRewardExchangePanel from './AdminRewardExchangePanel.vue'
import AdminExchangeTickets from './AdminExchangeTickets.vue'
import AdminPointsCDK from './AdminPointsCDK.vue'
import RewardExchangeOrders from './RewardExchangeOrders.vue'
import RewardLedgerPanel from './RewardLedgerPanel.vue'
const emit = defineEmits<{ openVersion: [id: string] }>()
const secondaryTabTheme = {
  barColor: 'transparent',
  tabFontSizeSmall: '13px',
  tabGapSmallBar: '6px',
  tabPaddingSmallBar: '6px 12px',
  tabTextColorBar: 'var(--text-dim)',
  tabFontWeightActive: '600',
  panePaddingSmall: '16px 0 0',
}
const tab = ref('rules'),
  rulesTab = ref('outcomes'),
  recordsTab = ref('users'),
  rules = ref<RewardRuleCatalogEntry[]>([]),
  more = ref(false),
  offset = ref(0),
  registration = ref<RegistrationRewardRule>(),
  selected = ref<AdminUser>(),
  userOpen = ref(false),
  audits = ref<AdminAuditLog[]>([]),
  auditCursor = ref(''),
  error = ref(''),
  busy = ref(false),
  saved = ref(false)
const route = useRoute()
watch(
  () => [route.query.pack, route.query.version],
  ([pack, version]) => {
    if (pack || version) {
      tab.value = 'records'
      recordsTab.value = 'ledger'
    }
  },
  { immediate: true },
)
const valid = computed(
  () =>
    !!registration.value &&
    [registration.value.points, registration.value.rewrite_credits].every(
      (n) => Number.isSafeInteger(n) && n >= 0 && n <= Number.MAX_SAFE_INTEGER,
    ),
)
async function loadRules(next = false) {
  busy.value = true
  error.value = ''
  try {
    const newOffset = next ? offset.value + 30 : 0
    const page = await api.rules(newOffset)
    rules.value = next ? [...rules.value, ...page.rules] : page.rules
    offset.value = newOffset
    more.value = page.has_more
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
async function loadRegistration() {
  busy.value = true
  error.value = ''
  saved.value = false
  try {
    registration.value = await api.registration()
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
async function save() {
  if (!registration.value || !valid.value) return
  busy.value = true
  error.value = ''
  saved.value = false
  try {
    registration.value = await api.saveRegistration(registration.value)
    saved.value = true
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
async function loadAudits(next = false) {
  busy.value = true
  error.value = ''
  try {
    const page = await api.audits(next ? auditCursor.value : undefined)
    audits.value = next ? [...audits.value, ...page.audits] : page.audits
    auditCursor.value = page.next_cursor
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
function select(user: AdminUser) {
  selected.value = user
  userOpen.value = true
}
function auditLabel(action: string) {
  return (
    (
      {
        reward_grant: '发放积分或次数',
        reward_reverse: '撤回发放',
        reward_registration_rule: '修改注册赠送',
        reward_exchange_offer: '修改兑换商品',
        reward_exchange_stock_add: '添加商品库存',
        reward_exchange_stock_remove: '移除商品库存',
        reward_exchange_ticket_refund: '工单退款',
        reward_exchange_ticket_reject: '驳回工单',
        reward_exchange_tickets_toggle: '切换工单开关',
        points_cdk_generate: '生成积分 CDK',
        reward_builtin_import: '导入玩法',
      } as Record<string, string>
    )[action] || action
  )
}
watch(rulesTab, (value) => {
  error.value = ''
  if (value === 'registration' && !registration.value) void loadRegistration()
})
watch(recordsTab, (value) => {
  error.value = ''
  if (value === 'audit') void loadAudits()
})
watch(tab, () => {
  error.value = ''
})
void loadRules()
</script>
<template>
  <div class="admin-rewards">
    <p class="intro">管理通关积分、改写套餐和用户收支。</p>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NTabs v-model:value="tab" type="line" animated>
      <NTabPane name="rules" tab="积分规则" display-directive="show:lazy"
        ><NTabs
          v-model:value="rulesTab"
          class="secondary-tabs"
          type="bar"
          size="small"
          :theme-overrides="secondaryTabTheme"
        >
          <NTabPane name="outcomes" tab="通关积分" display-directive="show:lazy"
            ><div class="toolbar">
              <p>积分随玩法版本发布。修改已发布玩法请先创建草稿，已有存档按原版本结算。</p>
              <NButton :loading="busy" @click="loadRules()">刷新</NButton>
            </div>
            <NSpin :show="busy && !rules.length"
              ><article v-for="rule in rules" :key="rule.id">
                <div>
                  <strong>{{ rule.pack_name }} · {{ rule.title }}</strong>
                  <p>
                    {{ rule.reward_points }} 积分 · {{ rule.version }} ·
                    {{
                      rule.current
                        ? '当前版本'
                        : rule.version_status === 'draft'
                          ? '草稿'
                          : '历史版本'
                    }}{{ rule.enabled ? '' : ' · 结局已停用' }}
                  </p>
                </div>
                <NButton @click="emit('openVersion', rule.challenge_version_id)"
                  >查看 / 编辑版本</NButton
                >
              </article>
              <NEmpty v-if="!busy && !error && !rules.length" description="暂无通关规则" /></NSpin
            ><NButton v-if="more" :loading="busy" @click="loadRules(true)"
              >加载更多</NButton
            ></NTabPane
          >
          <NTabPane name="registration" tab="注册赠送" display-directive="show:lazy"
            ><div class="toolbar">
              <p>只影响新账号。数量为 0 表示不赠送该项。</p>
              <NButton :loading="busy" @click="loadRegistration">重新读取</NButton>
            </div>
            <template v-if="registration"
              ><NFormItem label="赠送积分"
                ><NInputNumber
                  v-model:value="registration.points"
                  :min="0"
                  :max="Number.MAX_SAFE_INTEGER"
                  :precision="0" /></NFormItem
              ><NFormItem label="赠送剧情改写次数"
                ><NInputNumber
                  v-model:value="registration.rewrite_credits"
                  :min="0"
                  :max="Number.MAX_SAFE_INTEGER"
                  :precision="0" /></NFormItem
              ><NAlert v-if="saved" type="success">已保存，新注册用户将按此规则赠送。</NAlert
              ><NButton type="primary" :loading="busy" :disabled="!valid" @click="save"
                >保存注册赠送</NButton
              ></template
            ></NTabPane
          >
        </NTabs></NTabPane
      >
      <NTabPane name="exchange" tab="兑换商品" display-directive="show:lazy"
        ><AdminRewardExchangePanel
      /></NTabPane>
      <NTabPane name="tickets" tab="工单" display-directive="show:lazy"
        ><AdminExchangeTickets
      /></NTabPane>
      <NTabPane name="points-cdk" tab="积分 CDK" display-directive="show:lazy"
        ><AdminPointsCDK
      /></NTabPane>
      <NTabPane name="records" tab="用户与记录" display-directive="show:lazy"
        ><NTabs
          v-model:value="recordsTab"
          class="secondary-tabs"
          type="bar"
          size="small"
          :theme-overrides="secondaryTabTheme"
        >
          <NTabPane name="users" tab="用户查询" display-directive="show:lazy"
            ><p>搜索用户后，可发放积分、剧情改写次数或查看其收支。</p>
            <RewardUserSearch @select="select"
          /></NTabPane>
          <NTabPane name="ledger" tab="收支记录" display-directive="show:lazy"
            ><RewardLedgerPanel admin
          /></NTabPane>
          <NTabPane name="orders" tab="兑换订单" display-directive="show:lazy"
            ><RewardExchangeOrders admin
          /></NTabPane>
          <NTabPane name="audit" tab="管理操作" display-directive="show:lazy"
            ><div class="toolbar">
              <h3>管理操作记录</h3>
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
              <pre>{{ JSON.stringify(audit.metadata, null, 2) }}</pre>
            </details>
            <NEmpty v-if="!busy && !error && !audits.length" description="暂无管理操作" /><NButton
              v-if="auditCursor"
              :loading="busy"
              @click="loadAudits(true)"
              >加载更多</NButton
            ></NTabPane
          >
        </NTabs></NTabPane
      >
    </NTabs>
    <NModal
      v-model:show="userOpen"
      preset="card"
      title="用户账户资产"
      style="width: min(1000px, 96vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
      ><AdminRewardUserPanel v-if="selected" :key="selected.id" :user-id="selected.id"
    /></NModal>
  </div>
</template>
<style scoped>
.admin-rewards {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.secondary-tabs > :deep(.n-tabs-nav .n-tabs-tab) {
  border-radius: var(--radius);
}
.secondary-tabs > :deep(.n-tabs-nav .n-tabs-tab:hover) {
  background: var(--bg-soft);
}
.secondary-tabs > :deep(.n-tabs-nav .n-tabs-tab--active) {
  background: var(--accent-soft);
}
.intro,
p {
  color: var(--text-dim);
}
.toolbar,
article {
  display: flex;
  gap: 14px;
  align-items: center;
  justify-content: space-between;
}
article {
  padding: 16px 0;
  border-bottom: 1px solid var(--border-soft);
}
h3 {
  margin: 0;
}
details {
  padding: 14px 0;
  overflow-wrap: anywhere;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
p {
  margin: 8px 0 14px;
}
@media (max-width: 600px) {
  .toolbar,
  article {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
