<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NButton, NEmpty, NIcon, NTag, useDialog } from 'naive-ui'
import { AddOutline } from '@vicons/ionicons5'
import CharacterEditor from '@/components/CharacterEditor.vue'
import { useTemplateStore } from '@/stores/templates'
import type { Character } from '@/types'

const templates = useTemplateStore()
const dialog = useDialog()
const editing = ref<Partial<Character> | null>(null)

onMounted(() => templates.loadCharacters())

function create() {
  editing.value = {}
}
function edit(c: Character) {
  editing.value = { ...c }
}

async function save(value: Record<string, any>) {
  if (editing.value && editing.value.id) {
    await templates.updateCharacter(editing.value.id, value)
  } else {
    await templates.createCharacter(value)
  }
  editing.value = null
}

function remove(c: Character) {
  dialog.warning({
    title: '删除角色',
    content: `删除角色「${c.name}」？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: () => templates.removeCharacter(c.id),
  })
}
</script>

<template>
  <div class="page">
    <div class="row spread" style="align-items: baseline">
      <div>
        <div class="page-title">
          <h2>角色卡</h2>
          <span class="ornament">◆</span>
        </div>
        <p class="page-sub">茶馆的住客们——每张卡是一个可入梦的角色。</p>
      </div>
      <NButton type="primary" @click="create">
        <template #icon><NIcon :component="AddOutline" /></template>
        新建角色
      </NButton>
    </div>

    <div class="layout">
      <div class="list">
        <NEmpty
          v-if="!templates.characters.length"
          description="还没有角色卡。"
          style="padding: 40px 0"
        />
        <div
          v-for="(c, i) in templates.characters"
          :key="c.id"
          class="item fade-up"
          :class="{ active: editing && editing.id === c.id }"
          :style="{ animationDelay: Math.min(i * 40, 280) + 'ms' }"
        >
          <div class="row spread">
            <strong class="name">{{ c.name }}</strong>
            <div class="row item-actions" style="gap: 2px">
              <NButton quaternary size="tiny" @click="edit(c)">编辑</NButton>
              <NButton quaternary size="tiny" type="error" @click="remove(c)">删除</NButton>
            </div>
          </div>
          <div class="faint sm clamp">{{ c.description }}</div>
          <div class="row" style="gap: 5px; margin-top: 8px; flex-wrap: wrap">
            <NTag v-for="t in c.tags" :key="t" size="tiny" round :bordered="false">{{ t }}</NTag>
          </div>
        </div>
      </div>

      <div v-if="editing" class="edit-col">
        <CharacterEditor :model-value="editing" @save="save" @cancel="editing = null" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.page {
  max-width: 1020px;
  margin: 0 auto;
}

.layout {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 18px;
  margin-top: 6px;
}

.list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.item {
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 14px 16px;
  transition:
    border-color 0.2s ease,
    box-shadow 0.2s ease;
}

.item:hover {
  box-shadow: var(--shadow-card);
}

.item.active {
  border-color: var(--accent);
}

.name {
  font-size: 15px;
  letter-spacing: 1px;
}

.sm {
  font-size: 12.5px;
}

.clamp {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  margin-top: 3px;
}

.item-actions {
  opacity: 0;
  transition: opacity 0.15s ease;
}

.item:hover .item-actions {
  opacity: 1;
}

.edit-col {
  position: sticky;
  top: 0;
  align-self: start;
}
</style>
