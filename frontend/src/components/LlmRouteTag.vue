<script setup lang="ts">
import { NTag, NTooltip } from 'naive-ui'

defineProps<{ route?: string }>()
</script>

<template>
  <NTooltip v-if="route">
    <template #trigger>
      <NTag size="small" :bordered="false" :type="route === 'client' ? 'warning' : 'info'">
        {{ route === 'client' ? '直连' : '转发' }}
      </NTag>
    </template>
    {{ route === 'client' ? '客户端直连：模型输出未经服务端校验，不计奖励' : '服务端转发' }}
  </NTooltip>
</template>
