<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowForwardOutline,
  CheckmarkCircleOutline,
  LockClosedOutline,
  MailOutline,
  PeopleOutline,
  PersonOutline,
  ShieldCheckmarkOutline,
  SparklesOutline,
} from '@vicons/ionicons5'
import { NButton, NForm, NFormItem, NIcon, NInput, NSpin, useMessage } from 'naive-ui'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { USERNAME_RULE_HINT, useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const message = useMessage()

const identifier = ref('')
const email = ref('')
const username = ref('')
const password = ref('')
const passwordConfirm = ref('')
const displayName = ref('')
const verificationCode = ref('')
const invitationCode = ref('')
const submitting = ref(false)
const sendingCode = ref(false)
const codeCooldown = ref(0)
const mode = ref<'login' | 'register'>('login')
const error = ref(typeof route.query.error === 'string' ? route.query.error : '')
let cooldownTimer: number | undefined

const returnTo = computed(() => {
  const raw = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
  return raw.startsWith('/') && !raw.startsWith('//') ? raw : '/'
})

const passwordStrength = computed(() => {
  const value = password.value
  if (!value) return { score: 0, label: '输入密码后查看强度', tone: 'empty' }

  let score = 0
  if (value.length >= 8) score += 1
  if (value.length >= 12) score += 1
  if (/[A-Z]/.test(value) && /[a-z]/.test(value)) score += 1
  if (/\d/.test(value) || /[^\da-zA-Z]/.test(value)) score += 1

  if (score <= 1) return { score, label: '强度较弱', tone: 'weak' }
  if (score <= 2) return { score, label: '强度一般', tone: 'fair' }
  if (score === 3) return { score, label: '强度良好', tone: 'good' }
  return { score, label: '强度很高', tone: 'strong' }
})

onMounted(() => {
  auth
    .initialize()
    .then(() => auth.refreshConfig())
    .catch(() => {})
})

onUnmounted(() => {
  if (cooldownTimer) window.clearInterval(cooldownTimer)
})

function switchMode(nextMode: 'login' | 'register') {
  mode.value = nextMode
  error.value = ''
}

async function submitEmail() {
  await auth.initialize()
  if (mode.value === 'register' && !auth.config.email_registration_enabled) {
    message.warning('管理员尚未开放邮箱注册')
    return
  }
  if (mode.value === 'login' && !auth.config.email_auth_enabled) {
    message.warning('管理员尚未开放邮箱登录')
    return
  }

  submitting.value = true
  error.value = ''
  try {
    if (mode.value === 'register') {
      if (!username.value.trim()) {
        error.value = '请输入用户名'
        return
      }
      if (password.value !== passwordConfirm.value) {
        error.value = '两次输入的密码不一致'
        return
      }
      if (auth.config.email_verification_enabled && !verificationCode.value.trim()) {
        error.value = '请输入邮箱验证码'
        return
      }
      if (auth.config.invitation_required && !invitationCode.value.trim()) {
        error.value = '请输入邀请码'
        return
      }
      await auth.register(
        email.value,
        username.value,
        password.value,
        displayName.value,
        verificationCode.value,
        invitationCode.value,
      )
    } else {
      await auth.login(identifier.value, password.value)
    }
    await router.replace(returnTo.value)
  } catch (err: any) {
    error.value = err?.message || '登录失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}

async function sendCode() {
  await auth.initialize()
  if (!auth.config.email_registration_enabled) {
    message.warning('管理员尚未开放邮箱注册')
    return
  }
  if (!email.value.trim()) {
    message.warning('请先填写邮箱地址')
    return
  }
  sendingCode.value = true
  error.value = ''
  try {
    await auth.sendRegistrationCode(email.value)
    message.success('验证码已发送，请检查邮箱')
    codeCooldown.value = Math.max(auth.config.verification_resend_seconds, 30)
    if (cooldownTimer) window.clearInterval(cooldownTimer)
    cooldownTimer = window.setInterval(() => {
      codeCooldown.value -= 1
      if (codeCooldown.value <= 0 && cooldownTimer) {
        window.clearInterval(cooldownTimer)
        cooldownTimer = undefined
      }
    }, 1000)
  } catch (err: any) {
    error.value = err?.message || '验证码发送失败，请稍后重试'
  } finally {
    sendingCode.value = false
  }
}

async function startLinuxDO() {
  await auth.initialize()
  if (!auth.config.linuxdo_enabled) {
    message.warning('LinuxDo 登录不可用')
    return
  }
  window.location.assign(auth.linuxDOLoginURL(returnTo.value))
}
</script>

<template>
  <div class="login-page">
    <div class="login-backdrop" aria-hidden="true">
      <span class="backdrop-line line-one"></span>
      <span class="backdrop-line line-two"></span>
      <span class="backdrop-orbit orbit-one"></span>
      <span class="backdrop-orbit orbit-two"></span>
    </div>

    <div class="login-frame">
      <aside class="login-aside">
        <div class="aside-content">
          <div class="brand-lockup">
            <span class="brand-mark" aria-hidden="true">
              <span class="brand-mark-core"></span>
            </span>
            <div>
              <p class="brand-cn">命运茶馆</p>
              <p class="brand-en">FATECRAFT</p>
            </div>
          </div>

          <div class="aside-copy">
            <p class="eyebrow">A QUIET PLACE FOR STORIES</p>
            <h1>把每一次相遇，写成命运。</h1>
            <p class="aside-description">
              在角色、世界与想象之间，留下属于你的长篇故事。登录后，你的每一次选择都会被好好记住。
            </p>
          </div>

          <div class="aside-features">
            <div class="feature-item">
              <span class="feature-icon"
                ><NIcon><SparklesOutline /></NIcon
              ></span>
              <span><strong>沉浸式对话</strong><small>让角色拥有连续的记忆</small></span>
            </div>
            <div class="feature-item">
              <span class="feature-icon"
                ><NIcon><PeopleOutline /></NIcon
              ></span>
              <span><strong>专属世界</strong><small>整理你的角色卡与世界书</small></span>
            </div>
            <div class="feature-item">
              <span class="feature-icon"
                ><NIcon><ShieldCheckmarkOutline /></NIcon
              ></span>
              <span><strong>安心保存</strong><small>账号数据只属于你</small></span>
            </div>
          </div>
        </div>

        <div class="aside-footer">
          <span>FATECRAFT / 01</span>
          <span class="signal"><i></i> SYSTEM READY</span>
        </div>
      </aside>

      <section class="auth-panel" aria-labelledby="auth-title">
        <div class="panel-toolbar">
          <span class="panel-context">ACCOUNT GATE <span>/</span> 账号入口</span>
          <ThemeToggle />
        </div>

        <div class="panel-heading">
          <p class="section-kicker">
            {{ mode === 'login' ? 'WELCOME BACK' : 'BEGIN YOUR JOURNEY' }}
          </p>
          <h2 id="auth-title">{{ mode === 'login' ? '欢迎回来' : '创建你的账号' }}</h2>
          <p>{{ mode === 'login' ? '登录后继续你的故事。' : '准备好，让故事从这里开始。' }}</p>
        </div>

        <NForm class="auth-form" @submit.prevent="submitEmail">
          <NFormItem v-if="mode === 'register'" label="用户名" :feedback="USERNAME_RULE_HINT">
            <NInput v-model:value="username" size="large" placeholder="用于登录，注册后不可重复">
              <template #prefix>
                <NIcon><PersonOutline /></NIcon>
              </template>
            </NInput>
          </NFormItem>
          <NFormItem v-if="mode === 'register'" label="昵称（可选）">
            <NInput
              v-model:value="displayName"
              size="large"
              placeholder="你希望别人如何称呼你；留空时使用用户名"
            >
              <template #prefix>
                <NIcon><PersonOutline /></NIcon>
              </template>
            </NInput>
          </NFormItem>
          <NFormItem v-if="mode === 'register'" label="邮箱地址">
            <NInput
              v-model:value="email"
              size="large"
              type="text"
              inputmode="email"
              placeholder="name@example.com"
            >
              <template #prefix>
                <NIcon><MailOutline /></NIcon>
              </template>
            </NInput>
          </NFormItem>
          <NFormItem v-else label="用户名或邮箱">
            <NInput
              v-model:value="identifier"
              size="large"
              type="text"
              inputmode="email"
              placeholder="用户名或 name@example.com"
            >
              <template #prefix>
                <NIcon><MailOutline /></NIcon>
              </template>
            </NInput>
          </NFormItem>
          <NFormItem
            v-if="mode === 'register' && auth.config.email_verification_enabled"
            label="邮箱验证码"
          >
            <div class="code-row">
              <NInput
                v-model:value="verificationCode"
                size="large"
                maxlength="6"
                inputmode="numeric"
                placeholder="6 位验证码"
              >
                <template #prefix>
                  <NIcon><ShieldCheckmarkOutline /></NIcon>
                </template>
              </NInput>
              <NButton
                size="large"
                secondary
                :loading="sendingCode"
                :disabled="codeCooldown > 0"
                attr-type="button"
                @click="sendCode"
              >
                {{ codeCooldown > 0 ? `${codeCooldown}s` : '获取验证码' }}
              </NButton>
            </div>
          </NFormItem>
          <NFormItem v-if="mode === 'register' && auth.config.invitation_required" label="邀请码">
            <NInput
              v-model:value="invitationCode"
              size="large"
              placeholder="输入管理员提供的邀请码"
            >
              <template #prefix>
                <NIcon><LockClosedOutline /></NIcon>
              </template>
            </NInput>
          </NFormItem>
          <NFormItem label="密码">
            <NInput
              v-model:value="password"
              size="large"
              type="password"
              show-password-on="click"
              :placeholder="mode === 'register' ? '至少 8 位字符' : '输入你的密码'"
            >
              <template #prefix>
                <NIcon><LockClosedOutline /></NIcon>
              </template>
            </NInput>
          </NFormItem>

          <div v-if="mode === 'register'" class="password-strength" aria-live="polite">
            <div class="strength-track">
              <span
                v-for="index in 4"
                :key="index"
                :class="{
                  filled: index <= passwordStrength.score,
                  [passwordStrength.tone]: index <= passwordStrength.score,
                }"
              ></span>
            </div>
            <span class="strength-label" :class="passwordStrength.tone">{{
              passwordStrength.label
            }}</span>
          </div>

          <NFormItem v-if="mode === 'register'" label="确认密码">
            <NInput
              v-model:value="passwordConfirm"
              size="large"
              type="password"
              show-password-on="click"
              placeholder="再次输入密码"
            >
              <template #prefix>
                <NIcon><CheckmarkCircleOutline /></NIcon>
              </template>
            </NInput>
          </NFormItem>

          <div v-if="mode === 'login'" class="login-actions">
            <NButton
              class="submit-button"
              type="primary"
              size="large"
              block
              :loading="submitting"
              attr-type="submit"
            >
              <span>登录</span>
              <template #icon>
                <NIcon><ArrowForwardOutline /></NIcon>
              </template>
            </NButton>
            <NButton
              v-if="auth.config.linuxdo_enabled"
              class="linuxdo-button"
              type="primary"
              secondary
              size="large"
              block
              :loading="auth.loading"
              attr-type="button"
              @click="startLinuxDO"
            >
              <template #icon>
                <NIcon><PeopleOutline /></NIcon>
              </template>
              LinuxDo 登录
            </NButton>
          </div>
          <NButton
            v-else
            class="submit-button"
            type="primary"
            size="large"
            block
            :loading="submitting"
            attr-type="submit"
          >
            <span>创建账号</span>
            <template #icon>
              <NIcon><ArrowForwardOutline /></NIcon>
            </template>
          </NButton>

          <p class="mode-prompt">
            <template v-if="mode === 'login'">
              还没有账号？
              <button type="button" @click="switchMode('register')">注册</button>
            </template>
            <template v-else>
              已有账号？
              <button type="button" @click="switchMode('login')">返回登录</button>
            </template>
          </p>
        </NForm>

        <div class="panel-status">
          <NSpin v-if="auth.loading" size="small" />
          <span v-else
            ><NIcon><ShieldCheckmarkOutline /></NIcon> 你的账号信息会被安全保存</span
          >
        </div>
        <p v-if="error" class="login-error" role="alert">{{ error }}</p>
        <p class="panel-footnote">
          <NIcon><LockClosedOutline /></NIcon>
          {{ mode === 'register' ? '注册方式由管理员统一管理' : '使用已注册的账号登录' }}
        </p>
      </section>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  position: relative;
  min-height: 100%;
  display: grid;
  place-items: center;
  overflow: hidden;
  padding: 42px 28px;
  isolation: isolate;
}

.login-backdrop {
  position: absolute;
  inset: 0;
  pointer-events: none;
  overflow: hidden;
  z-index: -1;
}

.login-backdrop::before {
  content: '';
  position: absolute;
  inset: 7%;
  border: 1px solid color-mix(in srgb, var(--border-soft) 55%, transparent);
  opacity: 0.34;
}

.backdrop-line {
  position: absolute;
  display: block;
  height: 1px;
  width: 65vw;
  background: var(--accent-glow);
  transform: rotate(-27deg);
}

.line-one {
  top: 15%;
  left: -15%;
}

.line-two {
  right: -18%;
  bottom: 14%;
  transform: rotate(27deg);
}

.backdrop-orbit {
  position: absolute;
  display: block;
  width: min(42vw, 560px);
  aspect-ratio: 1;
  border: 1px solid var(--accent-glow);
  border-radius: 50%;
  opacity: 0.42;
}

.orbit-one {
  top: -28%;
  left: -12%;
}

.orbit-two {
  right: -18%;
  bottom: -35%;
  border-color: color-mix(in srgb, var(--border) 85%, transparent);
}

.login-frame {
  display: grid;
  grid-template-columns: minmax(300px, 0.92fr) minmax(420px, 1.08fr);
  width: min(100%, 1020px);
  min-height: 650px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--bg-panel) 94%, transparent);
  box-shadow:
    0 24px 80px color-mix(in srgb, var(--bg) 70%, transparent),
    var(--shadow-card);
}

.login-aside {
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  min-width: 0;
  padding: 50px 46px 30px;
  background: color-mix(in srgb, var(--bg-soft) 88%, var(--accent-soft));
  border-right: 1px solid var(--border-soft);
}

.login-aside::after {
  content: '';
  position: absolute;
  right: 34px;
  top: 104px;
  width: 1px;
  height: 170px;
  background: var(--accent);
  opacity: 0.5;
}

.aside-content {
  max-width: 340px;
}

.brand-lockup {
  display: flex;
  align-items: center;
  gap: 13px;
}

.brand-mark {
  position: relative;
  display: grid;
  width: 34px;
  height: 34px;
  place-items: center;
  border: 1px solid var(--accent);
  border-radius: 50%;
  box-shadow: inset 0 0 0 5px var(--accent-soft);
}

.brand-mark::before,
.brand-mark::after {
  content: '';
  position: absolute;
  width: 14px;
  height: 1px;
  background: var(--accent);
}

.brand-mark::after {
  transform: rotate(90deg);
}

.brand-mark-core {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 10px 2px var(--accent-glow);
}

.brand-cn,
.brand-en {
  margin: 0;
}

.brand-cn {
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 4px;
}

.brand-en {
  margin-top: 1px;
  color: var(--text-faint);
  font-size: 10px;
  letter-spacing: 3px;
}

.aside-copy {
  margin-top: 112px;
}

.eyebrow,
.section-kicker {
  margin: 0;
  color: var(--accent);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 2.5px;
}

.aside-copy h1 {
  max-width: 300px;
  margin: 16px 0 18px;
  font-size: clamp(28px, 3vw, 40px);
  font-weight: 600;
  letter-spacing: 2px;
  line-height: 1.35;
}

.aside-description {
  max-width: 310px;
  margin: 0;
  color: var(--text-dim);
  font-size: 13px;
  line-height: 1.9;
}

.aside-features {
  display: grid;
  gap: 17px;
  margin-top: 58px;
}

.feature-item {
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--text-dim);
}

.feature-icon {
  display: grid;
  width: 29px;
  height: 29px;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 50%;
  color: var(--accent);
  background: var(--accent-soft);
}

.feature-item strong,
.feature-item small {
  display: block;
}

.feature-item strong {
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
}

.feature-item small {
  margin-top: 1px;
  color: var(--text-faint);
  font-size: 11px;
}

.aside-footer {
  display: flex;
  justify-content: space-between;
  color: var(--text-faint);
  font-size: 9px;
  letter-spacing: 1.5px;
}

.signal {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.signal i {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--ok);
  box-shadow: 0 0 8px var(--ok);
}

.auth-panel {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 30px clamp(32px, 5vw, 66px) 28px;
  background: color-mix(in srgb, var(--bg-panel) 97%, transparent);
}

.panel-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 34px;
}

.panel-context {
  color: var(--text-faint);
  font-size: 10px;
  letter-spacing: 1.7px;
}

.panel-context span {
  padding: 0 5px;
  color: var(--accent);
}

.panel-heading {
  margin-top: 54px;
}

.panel-heading h2 {
  margin: 9px 0 4px;
  font-size: 28px;
  font-weight: 600;
  letter-spacing: 1px;
}

.panel-heading p:last-child {
  margin: 0;
  color: var(--text-faint);
  font-size: 13px;
}

.auth-form {
  flex: 0 0 auto;
  margin-top: 30px;
}

.auth-form :deep(.n-form-item) {
  margin-bottom: 17px;
}

.auth-form :deep(.n-form-item-label) {
  color: var(--text-dim);
  font-size: 12px;
}

.auth-form :deep(.n-input) {
  --n-border: var(--border);
  --n-border-hover: var(--accent);
  --n-border-focus: var(--accent);
  --n-box-shadow-focus: 0 0 0 2px var(--accent-soft);
  background: color-mix(in srgb, var(--bg) 38%, transparent);
}

.auth-form :deep(.n-input__input-el) {
  font-size: 13px;
}

.auth-form :deep(.n-input__prefix) {
  color: var(--text-faint);
}

.password-strength {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: -7px 0 15px;
}

.code-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 118px;
  gap: 9px;
  width: 100%;
}

.strength-track {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 3px;
  width: 108px;
}

.strength-track span {
  height: 3px;
  border-radius: 2px;
  background: var(--border);
  transition: background 0.2s ease;
}

.strength-track span.filled.weak {
  background: var(--danger);
}

.strength-track span.filled.fair {
  background: var(--accent);
}

.strength-track span.filled.good {
  background: var(--ok);
}

.strength-track span.filled.strong {
  background: var(--ok);
}

.strength-label {
  color: var(--text-faint);
  font-size: 11px;
}

.strength-label.weak {
  color: var(--danger);
}

.strength-label.fair {
  color: var(--accent);
}

.strength-label.good,
.strength-label.strong {
  color: var(--ok);
}

.submit-button {
  margin-top: 2px;
}

.login-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.linuxdo-button {
  margin-top: 2px;
}

.submit-button :deep(.n-button__icon) {
  order: 2;
  margin-left: 5px;
  margin-right: 0;
}

.mode-prompt {
  margin: 14px 0 0;
  color: var(--text-faint);
  font-size: 11px;
  text-align: center;
}

.mode-prompt button {
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--accent);
  cursor: pointer;
  font: inherit;
}

.mode-prompt button:hover {
  color: var(--accent-hover);
}

.panel-status {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 28px;
  margin-top: 17px;
  color: var(--text-faint);
  font-size: 11px;
}

.panel-status :deep(.n-icon) {
  color: var(--ok);
  vertical-align: -2px;
}

.login-error {
  margin: 8px 0 0;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--danger) 45%, transparent);
  border-radius: 4px;
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
  font-size: 12px;
  line-height: 1.5;
  text-align: center;
}

.panel-footnote {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  margin: auto 0 0;
  padding-top: 19px;
  color: var(--text-faint);
  font-size: 10px;
}

.panel-footnote :deep(.n-icon) {
  font-size: 12px;
}

@media (max-width: 800px) {
  .login-page {
    place-items: start center;
    overflow: auto;
    padding: 22px 14px;
  }

  .login-frame {
    grid-template-columns: 1fr;
    min-height: auto;
    width: min(100%, 560px);
  }

  .login-aside {
    min-height: 270px;
    padding: 29px 30px 25px;
    border-right: 0;
    border-bottom: 1px solid var(--border-soft);
  }

  .login-aside::after {
    display: none;
  }

  .aside-copy {
    margin-top: 43px;
  }

  .aside-copy h1 {
    max-width: none;
    margin: 10px 0 8px;
    font-size: 26px;
  }

  .aside-description {
    max-width: 500px;
    line-height: 1.7;
  }

  .aside-features {
    display: none;
  }

  .aside-footer {
    margin-top: 25px;
  }

  .auth-panel {
    padding: 25px 30px 26px;
  }

  .panel-heading {
    margin-top: 37px;
  }
}

@media (max-width: 460px) {
  .login-page {
    padding: 0;
  }

  .login-frame {
    width: 100%;
    border-right: 0;
    border-left: 0;
    border-radius: 0;
  }

  .login-aside {
    min-height: 0;
    padding: calc(18px + env(safe-area-inset-top)) 20px 18px;
  }

  .aside-copy {
    margin-top: 18px;
  }

  .aside-copy h1 {
    margin: 6px 0 0;
    font-size: 22px;
  }

  .aside-description,
  .aside-footer {
    display: none;
  }

  .auth-panel {
    padding: 20px 20px calc(22px + env(safe-area-inset-bottom));
  }

  .panel-context {
    font-size: 9px;
    letter-spacing: 1.2px;
  }

  .panel-heading h2 {
    font-size: 25px;
  }

  .panel-heading {
    margin-top: 24px;
  }

  .auth-form {
    margin-top: 22px;
  }

  .login-actions {
    grid-template-columns: 1fr;
    gap: 9px;
  }

  .code-row {
    grid-template-columns: minmax(0, 1fr) 108px;
  }
}
</style>
