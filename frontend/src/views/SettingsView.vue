<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import {
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NPopover,
  NSelect,
  NSlider,
  NSwitch,
  useMessage,
} from 'naive-ui'
import { useSettingsStore } from '@/stores/settings'
import { useThemeStore } from '@/stores/theme'
import { settingsApi } from '@/api'

const store = useSettingsStore()
const theme = useThemeStore()
const message = useMessage()
const form = ref<Record<string, any>>({})
const apiKey = ref('')
const fetchingModels = ref(false)
const availableModels = ref<string[]>([])

// 将模型列表转换为 NSelect 需要的选项格式
const modelOptions = computed(() => {
  return availableModels.value.map((model) => ({
    label: model,
    value: model,
  }))
})

onMounted(async () => {
  await store.load()
  if (store.settings) {
    form.value = { ...store.settings }
  }
})

async function save() {
  const body: Record<string, any> = {
    base_url: form.value.base_url,
    model: form.value.model,
    max_context_tokens: Number(form.value.max_context_tokens),
    max_response_tokens: Number(form.value.max_response_tokens),
    temperature: Number(form.value.temperature),
    top_p: Number(form.value.top_p),
    stream_enabled: !!form.value.stream_enabled,
    roll_system_enabled: !!form.value.roll_system_enabled,
    roll_difficulty_mode: form.value.roll_difficulty_mode || 'normal',
  }
  // 仅当填写了新 key 时才提交，避免清空已有密钥。
  if (apiKey.value) body.api_key = apiKey.value
  try {
    await store.update(body)
    apiKey.value = ''
    message.success('设置已保存')
  } catch (err: any) {
    message.error(`保存设置失败：${err.message || '未知错误'}`)
  }
}

async function fetchModels() {
  if (!form.value.base_url) {
    message.warning('请先填写 Base URL')
    return
  }

  fetchingModels.value = true
  try {
    // 通过后端代理请求，避免跨域问题
    const data = await settingsApi.fetchModels({
      base_url: form.value.base_url,
      api_key: apiKey.value || undefined, // 优先使用新填写的 key
    })

    if (!data.models || !Array.isArray(data.models)) {
      throw new Error('返回数据格式不正确')
    }

    const models = data.models
    if (models.length === 0) {
      message.warning('未找到可用模型')
      availableModels.value = []
      return
    }

    // 更新可用模型列表
    availableModels.value = models
    message.success(`获取到 ${models.length} 个模型`)

    // 如果当前模型为空，自动填充第一个
    if (!form.value.model && models[0]) {
      form.value.model = models[0]
      message.info(`已自动选择模型：${models[0]}`)
    }
  } catch (err: any) {
    message.error(`获取模型列表失败：${err.message}`)
    console.error('获取模型失败：', err)
    availableModels.value = []
  } finally {
    fetchingModels.value = false
  }
}
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h2>设置</h2>
      <span class="ornament">◆</span>
    </div>
    <p class="page-sub">叙事引擎与外观的调校台。</p>

    <section class="card">
      <div class="card-head">外观</div>
      <div class="row spread" style="padding: 4px 0">
        <div>
          <div>主题模式</div>
          <div class="faint sm">
            {{ theme.mode === 'dark' ? '夜茶馆（暗色）' : '宣纸（亮色）' }}
          </div>
        </div>
        <NButton secondary @click="theme.toggle()">
          {{ theme.mode === 'dark' ? '☀ 切换到宣纸' : '☾ 切换到夜茶馆' }}
        </NButton>
      </div>
    </section>

    <section class="card">
      <div class="card-head">LLM 接入</div>
      <NForm label-placement="top" size="small">
        <NFormItem label="Base URL（OpenAI 兼容）">
          <NInput v-model:value="form.base_url" placeholder="https://api.openai.com/v1" />
        </NFormItem>
        <NFormItem
          :label="`API Key${store.settings?.has_api_key ? '（已配置，留空则保持不变）' : ''}`"
        >
          <NInput
            v-model:value="apiKey"
            type="password"
            show-password-on="click"
            placeholder="sk-…"
          />
        </NFormItem>
        <NFormItem>
          <template #label>
            <div class="row spread" style="width: 100%">
              <span>模型</span>
              <NButton
                text
                type="primary"
                size="tiny"
                :loading="fetchingModels"
                @click="fetchModels"
              >
                获取模型列表
              </NButton>
            </div>
          </template>
          <NSelect
            v-if="availableModels.length > 0"
            v-model:value="form.model"
            :options="modelOptions"
            placeholder="请选择模型"
            filterable
            tag
          />
          <NInput v-else v-model:value="form.model" placeholder="请先获取模型列表" />
        </NFormItem>
        <NFormItem label="流式输出（逐字显示生成内容，关闭则等待完整回复）">
          <NSwitch v-model:value="form.stream_enabled">
            <template #checked>开启</template>
            <template #unchecked>关闭</template>
          </NSwitch>
        </NFormItem>
        <div class="row" style="gap: 16px">
          <NFormItem label="最大上下文 token" style="flex: 1">
            <NInputNumber
              v-model:value="form.max_context_tokens"
              :min="1024"
              :step="1024"
              style="width: 100%"
            />
          </NFormItem>
          <NFormItem label="最大回复 token" style="flex: 1">
            <NInputNumber
              v-model:value="form.max_response_tokens"
              :min="128"
              :step="128"
              style="width: 100%"
            />
          </NFormItem>
        </div>
        <NFormItem>
          <template #label>
            <span>temperature：{{ form.temperature ?? '—' }}</span>
            <NPopover trigger="hover" placement="top">
              <template #trigger>
                <span style="margin-left: 4px; cursor: help; opacity: 0.6">ⓘ</span>
              </template>
              <div style="max-width: 280px">
                控制输出的随机性。值越高，输出越有创造力和不可预测性；值越低，输出越稳定和确定性。<br />
                推荐范围：0.7-1.0（叙事）/ 0.2-0.5（逻辑任务）
              </div>
            </NPopover>
          </template>
          <NSlider v-model:value="form.temperature" :min="0" :max="2" :step="0.1" />
        </NFormItem>
        <NFormItem>
          <template #label>
            <span>top_p：{{ form.top_p ?? '—' }}</span>
            <NPopover trigger="hover" placement="top">
              <template #trigger>
                <span style="margin-left: 4px; cursor: help; opacity: 0.6">ⓘ</span>
              </template>
              <div style="max-width: 280px">
                核采样（Nucleus
                Sampling）参数，控制候选词汇的范围。值越小，模型只从最可能的词汇中选择；值越大，候选范围越广。<br />
                推荐范围：0.9-1.0（通常保持较高值）
              </div>
            </NPopover>
          </template>
          <NSlider v-model:value="form.top_p" :min="0" :max="1" :step="0.05" />
        </NFormItem>

        <!-- 判定系统配置 -->
        <div class="section-divider">
          <span>天命判定系统</span>
        </div>

        <NFormItem>
          <template #label>
            <span>启用判定系统</span>
            <NPopover trigger="hover" placement="top">
              <template #trigger>
                <span style="margin-left: 4px; cursor: help; opacity: 0.6">ⓘ</span>
              </template>
              <div style="max-width: 320px">
                启用后，AI 在遇到不确定事件（战斗、探索、炼丹等）时会发起天命判定（D100掷骰），
                根据角色属性、天赋、情境等因素计算成功率，让叙事更有紧张感和不可预测性。<br /><br />
                推荐搭配「轮回试炼」等预设包使用。
              </div>
            </NPopover>
          </template>
          <NSwitch v-model:value="form.roll_system_enabled">
            <template #checked>开启</template>
            <template #unchecked>关闭</template>
          </NSwitch>
        </NFormItem>

        <NFormItem v-if="form.roll_system_enabled" label="判定难度模式">
          <NSelect
            v-model:value="form.roll_difficulty_mode"
            :options="[
              { label: '简单（成功率 +15%）', value: 'easy' },
              { label: '普通（标准难度）', value: 'normal' },
              { label: '困难（成功率 -15%）', value: 'hard' },
            ]"
          />
        </NFormItem>
      </NForm>
      <div class="row" style="justify-content: flex-end">
        <NButton type="primary" @click="save">保存设置</NButton>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page {
  max-width: 620px;
  margin: 0 auto;
}

.card {
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 18px 22px;
  margin-bottom: 18px;
  box-shadow: var(--shadow-card);
}

.card-head {
  font-weight: 600;
  letter-spacing: 2px;
  margin-bottom: 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border-soft);
}

.sm {
  font-size: 12px;
}

.section-divider {
  margin: 24px 0 16px;
  text-align: center;
  position: relative;
}

.section-divider::before {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  height: 1px;
  background: var(--border-soft);
}

.section-divider span {
  position: relative;
  padding: 0 12px;
  background: var(--bg-panel);
  color: var(--text-faint);
  font-size: 13px;
  font-weight: 500;
  letter-spacing: 1px;
}
</style>
