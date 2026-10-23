<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NRadio,
  NRadioGroup,
  NSelect,
  NSwitch,
  NTag,
  useMessage,
} from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import { rewardError } from '@/lib/rewards'
import type { RewardExchangeOffer, RewardExchangeRevision } from '@/types/rewards'
import MarkdownEditor from './MarkdownEditor.vue'
import AdminExchangeStock from './AdminExchangeStock.vue'
const message = useMessage()
const offers = ref<RewardExchangeOffer[]>([]),
  form = ref<RewardExchangeOffer>(),
  open = ref(false),
  busy = ref(false),
  loading = ref(false),
  error = ref(''),
  formError = ref(''),
  revisions = ref<RewardExchangeRevision[]>([]),
  historyOpen = ref(false),
  historyError = ref(''),
  unlimited = ref(true),
  stockOffer = ref<RewardExchangeOffer>()
const kindOptions = [
  { label: 'CDK 商品', value: 'cdk' },
  { label: '站内奖励（剧情改写次数）', value: 'reward' },
]
const valid = computed(
  () =>
    !!form.value?.name.trim() &&
    Number.isSafeInteger(form.value.cost_quantity) &&
    form.value.cost_quantity > 0 &&
    Number.isSafeInteger(form.value.reward_quantity) &&
    form.value.reward_quantity > 0 &&
    (unlimited.value ||
      (Number.isSafeInteger(form.value.max_redemptions_per_user) &&
        form.value.max_redemptions_per_user > 0 &&
        form.value.max_redemptions_per_user <= Number.MAX_SAFE_INTEGER)),
)
async function load() {
  loading.value = true
  error.value = ''
  try {
    offers.value = await api.offers(true)
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    loading.value = false
  }
}
function edit(offer?: RewardExchangeOffer) {
  form.value = offer
    ? { ...offer }
    : {
        id: '',
        name: '',
        description: '',
        kind: 'cdk',
        tutorial: '',
        cost_quantity: 1,
        reward_quantity: 1,
        max_redemptions_per_user: 0,
        enabled: false,
        revision: 0,
        sort_order: 0,
        created_at: '',
        updated_at: '',
      }
  unlimited.value = form.value.max_redemptions_per_user === 0
  formError.value = ''
  open.value = true
}
// 切换到“限制份数”时给出可保存的默认值，避免 0 低于最小值。
function limitChanged(value: boolean) {
  if (!value && form.value && form.value.max_redemptions_per_user < 1)
    form.value.max_redemptions_per_user = 1
}
async function save() {
  if (!form.value || !valid.value) return
  busy.value = true
  formError.value = ''
  try {
    await api.save({
      ...form.value,
      max_redemptions_per_user: unlimited.value ? 0 : form.value.max_redemptions_per_user,
    })
    open.value = false
    message.success('兑换商品已保存')
    await load()
  } catch (err) {
    formError.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
async function history(offer: RewardExchangeOffer) {
  historyOpen.value = true
  historyError.value = ''
  revisions.value = []
  try {
    revisions.value = await api.revisions(offer.id)
  } catch (err) {
    historyError.value = rewardError(err)
  }
}
onMounted(load)
</script>
<template>
  <section class="exchange-admin">
    <div class="toolbar">
      <div>
        <h3>兑换商品</h3>
        <p>配置 CDK 或站内奖励商品并管理库存；商品只能下架，不能删除。</p>
      </div>
      <div class="buttons">
        <NButton :loading="loading" @click="load">刷新</NButton
        ><NButton type="primary" @click="edit()">新建商品</NButton>
      </div>
    </div>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <article v-for="offer in offers" :key="offer.id">
      <div>
        <strong>{{ offer.name }}</strong>
        <p>
          {{ offer.kind === 'cdk' ? 'CDK' : '站内奖励' }} · {{ offer.cost_quantity }} 积分{{
            offer.kind === 'reward' ? ` → ${offer.reward_quantity} 次剧情改写` : ' / 份'
          }}
          · 剩余 {{ offer.stock_remaining }} / {{ offer.stock_total }}
        </p>
        <p>
          {{
            offer.max_redemptions_per_user > 0
              ? `每人最多 ${offer.max_redemptions_per_user} 份`
              : '每人兑换不限'
          }}
        </p>
        <NTag :type="offer.enabled ? 'success' : 'default'">{{
          offer.enabled ? '已上架' : '已下架'
        }}</NTag
        ><small>修订 {{ offer.revision }}</small>
      </div>
      <div class="buttons">
        <NButton @click="stockOffer = offer">库存</NButton
        ><NButton @click="history(offer)">修订记录</NButton
        ><NButton @click="edit(offer)">编辑 / 上下架</NButton>
      </div>
    </article>
    <NEmpty v-if="!loading && !error && !offers.length" description="尚未配置兑换商品" />
    <NModal
      v-model:show="open"
      preset="card"
      title="兑换商品配置"
      :mask-closable="!busy"
      :closable="!busy"
      :close-on-esc="!busy"
      style="width: min(720px, 94vw)"
      ><template v-if="form"
        ><NAlert v-if="formError" type="error"
          >{{ formError }}
          <p>若商品已被其他管理员修改，请关闭后刷新，再重新编辑。</p></NAlert
        >
        <NForm label-placement="top" :show-feedback="false" class="offer-form">
          <fieldset>
            <legend>基本信息</legend>
            <div class="form-grid">
              <NFormItem label="商品类型"
                ><NSelect
                  v-model:value="form.kind"
                  :options="kindOptions"
                  :disabled="!!form.id"
                  placeholder="创建后不可修改"
              /></NFormItem>
              <NFormItem label="商品名称"
                ><NInput v-model:value="form.name" :maxlength="128" placeholder="必填"
              /></NFormItem>
            </div>
            <NFormItem label="说明"
              ><NInput v-model:value="form.description" type="textarea" :rows="2" :maxlength="2000"
            /></NFormItem>
          </fieldset>
          <fieldset>
            <legend>价格与额度</legend>
            <div class="form-grid">
              <NFormItem label="每份积分价格"
                ><NInputNumber
                  v-model:value="form.cost_quantity"
                  :min="1"
                  :max="Number.MAX_SAFE_INTEGER"
                  :precision="0"
                  update-value-on-input
                  class="number" /></NFormItem
              ><NFormItem v-if="form.kind === 'reward'" label="每份改写次数"
                ><NInputNumber
                  v-model:value="form.reward_quantity"
                  :min="1"
                  :max="Number.MAX_SAFE_INTEGER"
                  :precision="0"
                  update-value-on-input
                  class="number"
              /></NFormItem>
            </div>
            <div class="form-grid">
              <NFormItem label="每人兑换上限"
                ><NRadioGroup v-model:value="unlimited" @update:value="limitChanged"
                  ><NRadio :value="true">不限</NRadio
                  ><NRadio :value="false">限制份数</NRadio></NRadioGroup
                ></NFormItem
              ><NFormItem v-if="!unlimited" label="每人最多兑换份数"
                ><NInputNumber
                  v-model:value="form.max_redemptions_per_user"
                  :min="1"
                  :max="Number.MAX_SAFE_INTEGER"
                  :precision="0"
                  update-value-on-input
                  class="number"
              /></NFormItem>
            </div>
          </fieldset>
          <fieldset>
            <legend>上架</legend>
            <div class="form-grid">
              <NFormItem label="上架状态"
                ><NSwitch v-model:value="form.enabled"
                  ><template #checked>已上架</template
                  ><template #unchecked>已下架</template></NSwitch
                ></NFormItem
              ><NFormItem label="排序（较小靠前）"
                ><NInputNumber
                  v-model:value="form.sort_order"
                  :min="-2147483648"
                  :max="2147483647"
                  :precision="0"
                  update-value-on-input
                  class="number"
              /></NFormItem>
            </div>
          </fieldset>
          <fieldset>
            <legend>兑换成功后展示的使用教程（留空不展示）</legend>
            <MarkdownEditor v-model="form.tutorial" :maxlength="20000" />
          </fieldset>
        </NForm>
        <div class="form-footer">
          <p>保存会新增一份配置修订，已有订单保留当时的价格和奖励。</p>
          <NButton type="primary" :disabled="!valid" :loading="busy" @click="save"
            >保存商品</NButton
          >
        </div></template
      ></NModal
    >
    <NModal
      :show="!!stockOffer"
      preset="card"
      :title="`库存 · ${stockOffer?.name ?? ''}`"
      style="width: min(900px, 96vw)"
      @update:show="(show: boolean) => !show && ((stockOffer = undefined), load())"
      ><AdminExchangeStock v-if="stockOffer" :offer="stockOffer"
    /></NModal>
    <NModal
      v-model:show="historyOpen"
      preset="card"
      title="商品修订记录"
      style="width: min(760px, 94vw)"
      ><NAlert v-if="historyError" type="error">{{ historyError }}</NAlert>
      <div v-for="revision in revisions" :key="revision.revision" class="revision">
        <strong>修订 {{ revision.revision }} · {{ revision.snapshot.name }}</strong>
        <p>
          {{ new Date(revision.created_at).toLocaleString() }} ·
          {{ revision.snapshot.enabled ? '上架' : '下架' }}
        </p>
        <p>
          支付：{{ revision.snapshot.cost_quantity }} 积分；所得：{{
            revision.snapshot.reward_quantity
          }}
          次剧情改写
        </p>
        <p>
          每人兑换：
          {{
            revision.snapshot.max_redemptions_per_user > 0
              ? `最多 ${revision.snapshot.max_redemptions_per_user} 份`
              : '不限'
          }}
        </p>
        <small>操作者 {{ revision.actor_id || '系统初始化' }}</small>
      </div></NModal
    >
  </section>
</template>
<style scoped>
.exchange-admin {
  display: grid;
  gap: 20px;
}
.toolbar,
article {
  display: flex;
  gap: 14px;
  justify-content: space-between;
  align-items: center;
}
.buttons {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
article,
.revision {
  padding: 16px 0;
  border-bottom: 1px solid var(--border-soft);
}
p,
small {
  color: var(--text-dim);
}
small {
  margin-left: 8px;
}
.offer-form {
  display: grid;
  gap: 16px;
}
fieldset {
  display: grid;
  min-width: 0;
  gap: 12px;
  margin: 0;
  padding: 12px 16px 16px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
}
legend {
  padding: 0 6px;
  font-weight: 600;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px 16px;
  align-items: start;
}
.number {
  width: 100%;
}
.form-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
}
.form-footer p {
  margin: 0;
  font-size: 13px;
}
@media (max-width: 640px) {
  .toolbar,
  article {
    flex-direction: column;
    align-items: start;
  }
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
