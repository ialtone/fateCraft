<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NButton, NEmpty, NIcon, NInput, NTag, useDialog, useMessage } from 'naive-ui'
import { AddOutline } from '@vicons/ionicons5'
import WorldBookEntryEditor from '@/components/WorldBookEntryEditor.vue'
import { useWorldBookStore } from '@/stores/worldbooks'
import type { WorldBookEntry } from '@/types'

const store = useWorldBookStore()
const dialog = useDialog()
const message = useMessage()
const editingEntry = ref<Partial<WorldBookEntry> | null>(null)
const newBookName = ref('')

onMounted(() => store.loadList())

async function selectBook(id: string) {
  editingEntry.value = null
  await store.load(id)
}

async function createBook() {
  const name = newBookName.value.trim()
  if (!name) return
  try {
    const b = await store.create(name)
    newBookName.value = ''
    await selectBook(b.id)
  } catch (err: any) {
    message.error(`创建世界书失败：${err.message || '未知错误'}`)
  }
}

function removeBook(id: string, name: string) {
  dialog.warning({
    title: '删除世界书',
    content: `删除世界书「${name}」及其全部条目？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: () => store.remove(id),
  })
}

function newEntry() {
  editingEntry.value = {}
}

async function saveEntry(value: Record<string, any>) {
  if (!store.current) return
  try {
    if (editingEntry.value && editingEntry.value.id) {
      await store.updateEntry(store.current.id, editingEntry.value.id, value)
    } else {
      await store.addEntry(store.current.id, value)
    }
    editingEntry.value = null
  } catch (err: any) {
    message.error(`保存条目失败：${err.message || '未知错误'}`)
  }
}

function removeEntry(e: WorldBookEntry) {
  if (!store.current) return
  dialog.warning({
    title: '删除条目',
    content: `删除条目「${e.title}」？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: () => store.removeEntry(store.current!.id, e.id),
  })
}
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h2>世界书</h2>
      <span class="ornament">◆</span>
    </div>
    <p class="page-sub">世界观的典籍——常驻条目恒在，关键词条目闻声而现。</p>

    <div class="layout">
      <!-- 世界书列表 -->
      <div class="col books">
        <div class="row" style="gap: 8px; margin-bottom: 4px">
          <NInput
            v-model:value="newBookName"
            placeholder="新世界书名称"
            size="small"
            @keyup.enter="createBook"
          />
          <NButton type="primary" size="small" @click="createBook">
            <template #icon><NIcon :component="AddOutline" /></template>
          </NButton>
        </div>
        <div
          v-for="b in store.books"
          :key="b.id"
          class="book"
          :class="{ active: store.current?.id === b.id }"
          @click="selectBook(b.id)"
        >
          <div class="row spread">
            <strong class="book-name">{{ b.name }}</strong>
            <NButton
              quaternary
              size="tiny"
              type="error"
              class="del"
              @click.stop="removeBook(b.id, b.name)"
            >
              删
            </NButton>
          </div>
        </div>
      </div>

      <!-- 条目列表 -->
      <div class="col entries">
        <NEmpty
          v-if="!store.current"
          description="选择一个世界书查看条目。"
          style="padding: 40px 0"
        />
        <template v-else>
          <div class="row spread" style="margin-bottom: 4px">
            <strong class="col-title">{{ store.current.name }} · 条目</strong>
            <NButton type="primary" size="tiny" @click="newEntry">＋ 新条目</NButton>
          </div>
          <div v-for="e in store.current.entries" :key="e.id" class="entry">
            <div class="row spread">
              <span class="row" style="gap: 7px">
                <strong>{{ e.title }}</strong>
                <NTag v-if="e.constant" size="tiny" round type="warning" :bordered="false"
                  >常驻</NTag
                >
                <NTag v-if="!e.enabled" size="tiny" round :bordered="false">禁用</NTag>
              </span>
              <div class="row entry-actions" style="gap: 2px">
                <NButton quaternary size="tiny" @click="editingEntry = { ...e }">编辑</NButton>
                <NButton quaternary size="tiny" type="error" @click="removeEntry(e)">删除</NButton>
              </div>
            </div>
            <div class="faint sm" style="margin-top: 3px">
              关键词：{{ e.keywords.join('、') || '（无）' }} · {{ e.token_count ?? '?' }}t
            </div>
          </div>
        </template>
      </div>

      <!-- 条目编辑 -->
      <div class="col edit">
        <WorldBookEntryEditor
          v-if="editingEntry"
          :model-value="editingEntry"
          @save="saveEntry"
          @cancel="editingEntry = null"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.page {
  max-width: 1240px;
  margin: 0 auto;
}

.layout {
  display: grid;
  grid-template-columns: 250px 1fr 1fr;
  gap: 18px;
  align-items: start;
  margin-top: 6px;
}

.col {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.col-title {
  letter-spacing: 1px;
}

.book {
  position: relative;
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 11px 13px;
  cursor: pointer;
  transition: border-color 0.2s ease;
  overflow: hidden;
}

.book::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: transparent;
  transition: background 0.2s ease;
}

.book:hover {
  border-color: var(--border);
}

.book.active {
  border-color: var(--accent);
}

.book.active::before {
  background: var(--accent);
}

.book-name {
  letter-spacing: 1px;
}

.del {
  opacity: 0;
  transition: opacity 0.15s ease;
}

.book:hover .del {
  opacity: 1;
}

.entry {
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 12px 14px;
}

.entry-actions {
  opacity: 0;
  transition: opacity 0.15s ease;
}

.entry:hover .entry-actions {
  opacity: 1;
}

.sm {
  font-size: 12.5px;
}

.edit {
  position: sticky;
  top: 0;
}
</style>
