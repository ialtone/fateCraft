import { defineStore } from 'pinia'
import { ref } from 'vue'
import { worldbooksApi } from '@/api'
import type { WorldBook } from '@/types'

export const useWorldBookStore = defineStore('worldbook', () => {
  const books = ref<WorldBook[]>([])
  const current = ref<WorldBook | null>(null)

  async function loadList() {
    books.value = await worldbooksApi.list()
  }

  async function load(id: string) {
    current.value = await worldbooksApi.get(id)
    return current.value
  }

  async function create(name: string) {
    const b = await worldbooksApi.create({ name })
    books.value.unshift(b)
    return b
  }

  async function remove(id: string) {
    await worldbooksApi.remove(id)
    books.value = books.value.filter((b) => b.id !== id)
    if (current.value?.id === id) current.value = null
  }

  async function addEntry(bookId: string, body: Record<string, any>) {
    await worldbooksApi.addEntry(bookId, body)
    await load(bookId)
  }

  async function updateEntry(bookId: string, entryId: string, body: Record<string, any>) {
    await worldbooksApi.updateEntry(entryId, body)
    await load(bookId)
  }

  async function removeEntry(bookId: string, entryId: string) {
    await worldbooksApi.removeEntry(entryId)
    await load(bookId)
  }

  return { books, current, loadList, load, create, remove, addEntry, updateEntry, removeEntry }
})
