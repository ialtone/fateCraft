<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NEmpty,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NRadio,
  NRadioGroup,
  NSwitch,
  NTag,
  useMessage,
} from 'naive-ui'
import { rewardExchangeApi as api } from '@/api/rewards'
import { rewardError } from '@/lib/rewards'
import type { RewardExchangeOffer, RewardExchangeRevision } from '@/types/rewards'
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
  unlimited = ref(true)
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
        <p>设置积分价格和所得剧情改写次数；上架后玩家可在“我的奖励”兑换。</p>
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
          {{ offer.cost_quantity }} 积分 → {{ offer.reward_quantity }}
          次剧情改写
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
        <NButton @click="history(offer)">修订记录</NButton
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
        <NFormItem label="商品名称"><NInput v-model:value="form.name" :maxlength="128" /></NFormItem
        ><NFormItem label="说明"
          ><NInput v-model:value="form.description" type="textarea" :maxlength="2000"
        /></NFormItem>
        <div class="form-grid">
          <NFormItem label="每份积分价格"
            ><NInputNumber
              v-model:value="form.cost_quantity"
              :min="1"
              :max="Number.MAX_SAFE_INTEGER"
              :precision="0" /></NFormItem
          ><NFormItem label="每份改写次数"
            ><NInputNumber
              v-model:value="form.reward_quantity"
              :min="1"
              :max="Number.MAX_SAFE_INTEGER"
              :precision="0" /></NFormItem
          ><NFormItem label="每人兑换上限"
            ><NRadioGroup v-model:value="unlimited"
              ><NRadio :value="true">不限</NRadio
              ><NRadio :value="false">限制份数</NRadio></NRadioGroup
            ></NFormItem
          ><NFormItem v-if="!unlimited" label="每人最多兑换份数"
            ><NInputNumber
              v-model:value="form.max_redemptions_per_user"
              :min="1"
              :max="Number.MAX_SAFE_INTEGER"
              :precision="0" /></NFormItem
          ><NFormItem label="上架"><NSwitch v-model:value="form.enabled" /></NFormItem
          ><NFormItem label="排序（较小靠前）"
            ><NInputNumber
              v-model:value="form.sort_order"
              :min="-2147483648"
              :max="2147483647"
              :precision="0"
          /></NFormItem>
        </div>
        <p>保存会新增一份配置修订，已有订单保留当时的价格和奖励。</p>
        <NButton type="primary" :disabled="!valid" :loading="busy" @click="save"
          >保存商品</NButton
        ></template
      ></NModal
    >
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
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
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
