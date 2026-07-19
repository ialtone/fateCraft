<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { NButton, NEmpty } from 'naive-ui'
</script>

<template>
  <div class="notfound">
    <NEmpty size="large" description="此处天机未开——页面不存在。">
      <template #extra>
        <RouterLink to="/">
          <NButton type="primary" size="small">返回茶馆</NButton>
        </RouterLink>
      </template>
    </NEmpty>
  </div>
</template>

<style scoped>
.notfound {
  min-height: 100%;
  display: grid;
  place-items: center;
}
</style>
