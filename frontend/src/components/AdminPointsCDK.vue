<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NAlert, NButton, NEmpty, NFormItem, NInput, NInputNumber, NModal, NSelect } from 'naive-ui'
import { pointsCDKApi as api } from '@/api/rewards'
import { rewardError } from '@/lib/rewards'
import type { PointsCDK, PointsCDKBatch } from '@/types/rewards'
const batches = ref<PointsCDKBatch[]>([]),
  dayOptions = ref([1, 3, 7, 15, 30].map((d) => ({ label: `${d} 天`, value: d }))),
  form = ref({ name: '', points: 100, quantity: 10, days: 7 }),
  codes = ref<PointsCDK[]>([]),
  codesTitle = ref(''),
  busy = ref(false),
  error = ref('')
async function run(action: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  try {
    await action()
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
const load = () =>
  run(async () => {
    const result = await api.batches()
    batches.value = result.batches
    dayOptions.value = result.expiry_days.map((d) => ({ label: `${d} 天`, value: d }))
  })
const generate = () =>
  run(async () => {
    const result = await api.generate(form.value)
    codesTitle.value = result.batch.name
    codes.value = result.codes
    await load()
  })
const show = (batch: PointsCDKBatch) =>
  run(async () => {
    codes.value = await api.codes(batch.id)
    codesTitle.value = batch.name
  })
onMounted(load)
</script>
<template>
  <section class="points-cdk">
    <div class="form">
      <NFormItem label="批次名称"><NInput v-model:value="form.name" :maxlength="128" /></NFormItem
      ><NFormItem label="面值（积分）"
        ><NInputNumber
          v-model:value="form.points"
          :min="1"
          :precision="0"
          update-value-on-input
          class="number" /></NFormItem
      ><NFormItem label="数量"
        ><NInputNumber
          v-model:value="form.quantity"
          :min="1"
          :max="1000"
          :precision="0"
          update-value-on-input
          class="number" /></NFormItem
      ><NFormItem label="有效期"
        ><NSelect v-model:value="form.days" :options="dayOptions"
      /></NFormItem>
    </div>
    <NButton
      class="generate"
      type="primary"
      :loading="busy"
      :disabled="!form.name.trim() || !form.points || !form.quantity"
      @click="generate"
      >生成 CDK</NButton
    >
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <article v-for="batch in batches" :key="batch.id">
      <div>
        <strong>{{ batch.name }}</strong>
        <p>
          {{ batch.points }} 积分 · 已兑换 {{ batch.redeemed }} / {{ batch.quantity }} · 有效期至
          {{ new Date(batch.expires_at).toLocaleString() }}
        </p>
      </div>
      <NButton @click="show(batch)">查看 CDK</NButton>
    </article>
    <NEmpty v-if="!busy && !batches.length" description="尚未生成积分 CDK" />
    <NModal
      :show="!!codesTitle"
      preset="card"
      :title="`积分 CDK · ${codesTitle}`"
      style="width: min(760px, 94vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
      @update:show="(v: boolean) => !v && (codesTitle = '')"
    >
      <p v-for="code in codes" :key="code.code" class="code">
        <code>{{ code.code }}</code
        ><span class="meta">{{
          code.redeemed_by
            ? `@${code.username || code.redeemed_by} · ${new Date(code.redeemed_at!).toLocaleString()}`
            : '未使用'
        }}</span>
      </p>
    </NModal>
  </section>
</template>
<style scoped>
.points-cdk {
  display: grid;
  gap: 14px;
}
.form {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}
article,
.code {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  padding: 10px 0;
  border-bottom: 1px solid var(--border-soft);
}
p,
small {
  margin: 0;
  color: var(--text-dim);
}
.meta {
  color: var(--text-dim);
  font-size: 14px;
}
.number {
  width: 100%;
}
.generate {
  justify-self: start;
}
@media (max-width: 640px) {
  .form {
    grid-template-columns: 1fr;
  }
}
</style>
