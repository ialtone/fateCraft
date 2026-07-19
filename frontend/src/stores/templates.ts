import { defineStore } from 'pinia'
import { ref } from 'vue'
import { charactersApi, personasApi, scenariosApi } from '@/api'
import type { Character, Persona, Scenario } from '@/types'

export const useTemplateStore = defineStore('template', () => {
  const characters = ref<Character[]>([])
  const personas = ref<Persona[]>([])
  const scenarios = ref<Scenario[]>([])

  async function loadAll() {
    const [c, p, s] = await Promise.all([
      charactersApi.list(),
      personasApi.list().catch(() => []),
      scenariosApi.list().catch(() => []),
    ])
    characters.value = c
    personas.value = p
    scenarios.value = s
  }

  async function loadCharacters() {
    characters.value = await charactersApi.list()
  }

  async function createCharacter(body: Record<string, any>) {
    const ch = await charactersApi.create(body)
    characters.value.unshift(ch)
    return ch
  }

  async function updateCharacter(id: string, body: Record<string, any>) {
    const ch = await charactersApi.update(id, body)
    const idx = characters.value.findIndex((x) => x.id === id)
    if (idx >= 0) characters.value[idx] = ch
    return ch
  }

  async function removeCharacter(id: string) {
    await charactersApi.remove(id)
    characters.value = characters.value.filter((x) => x.id !== id)
  }

  async function createPersona(body: Record<string, any>) {
    const p = await personasApi.create(body)
    personas.value.unshift(p)
    return p
  }

  return {
    characters,
    personas,
    scenarios,
    loadAll,
    loadCharacters,
    createCharacter,
    updateCharacter,
    removeCharacter,
    createPersona,
  }
})
