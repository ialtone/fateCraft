<script setup lang="ts">
import SharedMessageItem from '@/components/SharedMessageItem.vue'
import type { ConversationShareSnapshot } from '@/types'

defineProps<{ snapshot: ConversationShareSnapshot }>()
</script>

<template>
  <div class="shared-content">
    <SharedMessageItem
      v-for="message in snapshot.messages"
      :key="message.position"
      :message="message"
    />
  </div>
</template>

<style scoped>
.shared-content {
  display: grid;
  gap: 16px;
}
</style>
