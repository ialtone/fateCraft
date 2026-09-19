<script setup lang="ts">
import FieldHelp from '@/components/FieldHelp.vue'
import type { LLMCapabilityRole } from '@/types'

const props = withDefaults(
  defineProps<{
    role: LLMCapabilityRole
    label?: string
  }>(),
  {
    label: undefined,
  },
)

const modelCopy: Record<LLMCapabilityRole, { label: string; title: string; paragraphs: string[] }> =
  {
    narrative: {
      label: '叙事模型',
      title: '主叙事模型',
      paragraphs: [
        '负责生成玩家看见的剧情回复，并理解玩法规则、上下文和模型动作。',
        '建议优先选择能力最强、上下文窗口足够大的模型；长篇复杂玩法更适合高级模型。它决定主要的叙事质量，费用和响应时间通常也最高。',
      ],
    },
    memory: {
      label: '长期记忆模型',
      title: '后台长期记忆整理模型',
      paragraphs: [
        '在回合结束后异步归纳已发生的内容，帮助后续上下文保持连贯，不会阻塞当前叙事回复。',
        '模型名称留空时，系统会自动使用上方配置的叙事模型，并非停用长期记忆。需要单独控制成本时，再填写便宜、稳定、支持原生 JSON Schema 的中小模型。',
      ],
    },
    character_inquiry: {
      label: '角色问答模型',
      title: '左侧只读角色问答模型',
      paragraphs: [
        '回答用户对当前角色、状态、任务和事件的查询，只读，不会修改剧情或玩法状态。',
        '模型名称留空时，系统会自动使用上方配置的叙事模型，并非停用角色问答。需要单独优化响应速度或成本时，再填写其他模型。',
      ],
    },
  }

const copy = modelCopy[props.role]
</script>

<template>
  <FieldHelp :label="label || copy.label" :aria-label="`${label || copy.label}说明`">
    <strong>{{ copy.title }}</strong>
    <p v-for="paragraph in copy.paragraphs" :key="paragraph">{{ paragraph }}</p>
  </FieldHelp>
</template>
