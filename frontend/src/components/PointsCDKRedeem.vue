<script setup lang="ts">
import { ref } from 'vue'
import { NAlert, NButton, NInput } from 'naive-ui'
import { pointsCDKApi } from '@/api/rewards'
import { rewardError } from '@/lib/rewards'
const emit = defineEmits<{ redeemed: [] }>()
const code = ref(''),
  busy = ref(false),
  error = ref(''),
  done = ref('')
async function redeem() {
  if (busy.value || !code.value.trim()) return
  busy.value = true
  error.value = done.value = ''
  try {
    done.value = `兑换成功，获得 ${await pointsCDKApi.redeem(code.value)} 积分`
    code.value = ''
    emit('redeemed')
  } catch (err) {
    error.value = rewardError(err)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <form class="cdk-redeem" @submit.prevent="redeem">
    <NInput v-model:value="code" :maxlength="64" placeholder="输入积分 CDK" aria-label="积分 CDK" />
    <NButton attr-type="submit" type="primary" :loading="busy" :disabled="!code.trim()"
      >兑换积分</NButton
    >
  </form>
  <NAlert v-if="error" type="error">{{ error }}</NAlert
  ><NAlert v-if="done" type="success">{{ done }}</NAlert>
</template>
<style scoped>
.cdk-redeem {
  display: flex;
  gap: 10px;
  max-width: 480px;
}
</style>
