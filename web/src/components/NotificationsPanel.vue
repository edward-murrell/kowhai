<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { api, call, type Notification } from '../api/client'
import { actorName, describe } from '../lib/activity'
import { ago } from '../lib/dates'
import { useSession } from '../stores/session'
import Avatar from './Avatar.vue'

const session = useSession()
const router = useRouter()
const open = ref(false)
const items = ref<Notification[]>([])
const cursor = ref<string | undefined>()
const loading = ref(false)

async function load(more = false) {
  loading.value = true
  try {
    const page = await call(api.GET('/me/notifications', { params: { query: { cursor: more ? cursor.value : undefined, limit: 20 } } }))
    items.value = more ? [...items.value, ...page.items] : page.items
    cursor.value = page.next_cursor
    session.unread = page.unread
  } catch (e) {
    session.error(e)
  } finally {
    loading.value = false
  }
}

watch(open, (v) => v && load())
watch(
  () => session.latest,
  (n) => {
    if (n && open.value) items.value.unshift(n)
  },
)

async function go(n: Notification) {
  if (!n.read) {
    await call(api.PATCH('/me/notifications/{id}', { params: { path: { id: n.id } }, body: { read: true } })).catch(() => {})
    n.read = true
  }
  open.value = false
  const a = n.activity
  if (a.card_id && a.type !== 'card.deleted') router.push(`/c/${a.card_id}`)
  else if (a.board_id) router.push(`/b/${a.board_id}`)
  else router.push(`/w/${a.workspace_id}`)
}

async function readAll() {
  await call(api.POST('/me/notifications/read-all')).catch(session.error)
  items.value.forEach((n) => (n.read = true))
  session.unread = 0
}
</script>

<template>
  <PopoverRoot v-model:open="open">
    <PopoverTrigger class="relative rounded p-1.5 hover:bg-white/10" :aria-label="`Notifications, ${session.unread} unread`">
      <svg viewBox="0 0 24 24" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
        <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" /><path d="M13.7 21a2 2 0 0 1-3.4 0" />
      </svg>
      <span
        v-if="session.unread"
        data-testid="unread-count"
        class="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-kowhai-400 px-1 text-center text-[10px] font-bold leading-4 text-bush-900"
        >{{ session.unread > 99 ? '99+' : session.unread }}</span
      >
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent class="z-50 w-96 rounded-lg bg-white text-stone-900 shadow-xl ring-1 ring-stone-900/10" align="end" :side-offset="8">
        <div class="flex items-center justify-between border-b border-stone-200 px-4 py-2">
          <span class="font-semibold">Notifications</span>
          <button class="text-xs text-bush-700 hover:underline" @click="readAll">Mark all read</button>
        </div>
        <ul class="max-h-[60vh] overflow-y-auto">
          <li v-if="!items.length && !loading" class="px-4 py-8 text-center text-sm text-stone-500">Nothing yet.</li>
          <li v-for="n in items" :key="n.id">
            <button class="flex w-full gap-3 px-4 py-3 text-left hover:bg-stone-50" :class="{ 'bg-kowhai-50': !n.read }" @click="go(n)">
              <Avatar :name="actorName(n.activity)" :url="n.activity.actor?.avatar_url" :size="28" />
              <span class="min-w-0 flex-1 text-sm">
                <span class="font-semibold">{{ actorName(n.activity) }}</span>
                {{ describe(n.activity) }}
                <span class="mt-0.5 block text-xs text-stone-500">{{ ago(n.created_at) }}</span>
              </span>
              <span v-if="!n.read" class="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-kowhai-400" aria-label="unread" />
            </button>
          </li>
        </ul>
        <button v-if="cursor" class="w-full border-t border-stone-200 py-2 text-sm text-bush-700 hover:bg-stone-50" @click="load(true)">
          Load more
        </button>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>
