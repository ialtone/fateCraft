<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NSpin, useMessage } from 'naive-ui'
import { USERNAME_RULE_HINT, useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const message = useMessage()
const email = ref('')
const username = ref('')
const password = ref('')
const passwordConfirm = ref('')
const confirmed = ref(false)
const submitting = ref(false)
const loading = ref(true)
const error = ref('')

onMounted(async () => {
  try {
    await auth.initialize()
    if (auth.installationInitialized !== false) {
      await router.replace({ name: 'conversations' })
    }
  } catch (err: any) {
    error.value = err?.message || '读取安装状态失败'
  } finally {
    loading.value = false
  }
})

async function complete() {
  if (!confirmed.value) return
  submitting.value = true
  error.value = ''
  try {
    if (
      !username.value.trim() ||
      password.value.length < 8 ||
      password.value !== passwordConfirm.value
    ) {
      error.value = password.value.length < 8 ? '密码至少需要 8 位字符' : '请检查用户名和两次密码'
      return
    }
    await auth.initializeOwner(email.value, password.value, username.value)
    message.success('平台初始化完成')
    await router.replace({ name: 'admin' })
  } catch (err: any) {
    error.value = err?.message || '初始化失败，请稍后重试'
    await auth.refreshInstallation().catch(() => {})
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="setup-page">
    <NCard class="setup-card" title="首次安装">
      <NSpin :show="loading">
        <NAlert v-if="auth.setupBlockedReason" type="error" title="无法完成首次安装">
          {{ auth.setupBlockedReason }}
        </NAlert>
        <template v-else-if="auth.installationInitialized === false">
          <p class="lead">当前实例还没有平台拥有者。</p>
          <p>
            请创建第一个本地账号。这个账号会成为唯一的
            <strong>owner</strong>，负责平台设置和管理员管理。
          </p>
          <NForm label-placement="top" @submit.prevent="complete">
            <NFormItem label="用户名" :feedback="USERNAME_RULE_HINT">
              <NInput v-model:value="username" placeholder="例如：掌柜" />
            </NFormItem>
            <NFormItem label="邮箱地址（可选）">
              <NInput
                v-model:value="email"
                inputmode="email"
                placeholder="用于联系，也可用于登录"
              />
            </NFormItem>
            <NFormItem label="密码">
              <NInput
                v-model:value="password"
                type="password"
                show-password-on="click"
                placeholder="至少 8 位字符"
              />
            </NFormItem>
            <NFormItem label="确认密码">
              <NInput v-model:value="passwordConfirm" type="password" show-password-on="click" />
            </NFormItem>
          </NForm>
          <NAlert type="warning" :show-icon="true">
            这是一次性的安装操作。完成后普通注册会开放，后续新用户默认都是普通用户。
          </NAlert>
          <label class="confirm"
            ><input v-model="confirmed" type="checkbox" /> 我确认这个账号将成为平台拥有者</label
          >
          <p v-if="error" class="setup-error" role="alert">{{ error }}</p>
          <div class="actions">
            <NButton type="primary" :disabled="!confirmed" :loading="submitting" @click="complete">
              完成安装并进入管理
            </NButton>
          </div>
        </template>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped>
.setup-page {
  display: grid;
  min-height: 100%;
  padding: 32px 18px;
  place-items: center;
}

.setup-card {
  width: min(100%, 560px);
}

.lead {
  margin-top: 0;
  font-size: 16px;
}

.confirm {
  margin-top: 22px;
}

.actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 24px;
}

.setup-error {
  color: var(--error, #c2410c);
}
</style>
