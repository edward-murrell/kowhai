<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, call, type Activity } from '../api/client'
import { actorName, describe } from '../lib/activity'
import { ago } from '../lib/dates'
import { useBoard } from '../stores/board'
import Avatar from './Avatar.vue'

const emit = defineEmits<{ close: []; open: [string] }>()
const board = useBoard()
const older = ref<Activity[]>([])
const cursor = ref<string | undefined>()

async function load(more = false) {
  const page = await call(
    api.GET('/boards/{id}/activity', { params: { path: { id: board.board!.id }, query: { cursor: more ? cursor.value : undefined, limit: 40 } } }),
  )
  older.value = more ? [...older.value, ...page.items] : page.items
  cursor.value = page.next_cursor
  board.feed = []
}
onMounted(() => load())

function items(): Activity[] {
  const seen = new Set(older.value.map((a) => a.id))
  return [...board.feed.filter((a) => !seen.has(a.id)), ...older.value]
}
</script>

<template>
  <aside class="flex flex-col border-l border-stone-200 bg-white" aria-label="Board activity">
    <div class="flex items-center justify-between border-b border-stone-200 px-4 py-2">
      <span class="font-semibold">Activity</span>
      <button class="btn-ghost px-2" aria-label="Close activity" @click="emit('close')">✕</button>
    </div>
    <ul class="flex-1 space-y-3 overflow-y-auto p-4">
      <li v-for="a in items()" :key="a.id" class="flex gap-2 text-sm">
        <Avatar :name="actorName(a)" :url="a.actor?.avatar_url" :size="24" />
        <div class="min-w-0">
          <span class="font-semibold">{{ actorName(a) }}</span>
          <button v-if="a.card_id && a.type !== 'card.deleted'" class="text-left hover:underline" @click="emit('open', a.card_id)">
            {{ describe(a) }}
          </button>
          <span v-else> {{ describe(a) }}</span>
          <div class="text-xs text-stone-500">{{ ago(a.created_at) }}</div>
        </div>
      </li>
    </ul>
    <button v-if="cursor" class="border-t border-stone-200 py-2 text-sm text-bush-700 hover:bg-stone-50" @click="load(true)">Load more</button>
  </aside>
</template>
