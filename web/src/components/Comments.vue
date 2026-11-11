<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { api, call, type Comment } from '../api/client'
import { ago, longDate } from '../lib/dates'
import { useSession } from '../stores/session'
import Avatar from './Avatar.vue'
import Markdown from './Markdown.vue'
import MarkdownEditor from './MarkdownEditor.vue'

const props = defineProps<{ cardId: string; tick: number; editable: boolean; isAdmin: boolean }>()
const session = useSession()
const items = ref<Comment[]>([])
const cursor = ref<string | undefined>()
const body = ref('')
const editing = ref('')
const editBody = ref('')
const busy = ref(false)

async function load(more = false) {
  const page = await call(api.GET('/cards/{id}/comments', { params: { path: { id: props.cardId }, query: { cursor: more ? cursor.value : undefined } } }))
  items.value = more ? [...items.value, ...page.items] : page.items
  cursor.value = page.next_cursor
}

onMounted(() => load().catch(session.error))
watch(() => props.tick, () => load().catch(() => {}))

async function post() {
  if (!body.value.trim()) return
  busy.value = true
  try {
    await call(api.POST('/cards/{id}/comments', { params: { path: { id: props.cardId } }, body: { body: body.value } }))
    body.value = ''
    await load()
  } catch (e) {
    session.error(e)
  } finally {
    busy.value = false
  }
}

async function save(c: Comment) {
  try {
    await call(api.PATCH('/comments/{id}', { params: { path: { id: c.id }, header: { 'If-Match': String(c.version) } }, body: { body: editBody.value } }))
    editing.value = ''
    await load()
  } catch (e) {
    session.error(e)
  }
}

async function remove(c: Comment) {
  if (!confirm('Delete this comment?')) return
  await call(api.DELETE('/comments/{id}', { params: { path: { id: c.id } } })).catch(session.error)
  await load()
}
</script>

<template>
  <section class="space-y-4" aria-label="Comments">
    <div v-if="editable" class="space-y-2">
      <MarkdownEditor v-model="body" :rows="3" placeholder="Write a comment…" label="New comment" @submit="post" />
      <button class="btn-primary" :disabled="busy || !body.trim()" @click="post">Comment</button>
    </div>
    <article v-for="c in items" :key="c.id" class="flex gap-3" data-testid="comment">
      <Avatar :name="c.author.display_name" :url="c.author.avatar_url" />
      <div class="min-w-0 flex-1">
        <div class="text-sm">
          <span class="font-semibold">{{ c.author.display_name }}</span>
          <span v-if="c.actor_label" class="ml-1 rounded bg-purple-100 px-1 text-xs text-purple-800">{{ c.actor_label }}</span>
          <span class="ml-2 text-xs text-stone-500" :title="longDate(c.created_at)">{{ ago(c.created_at) }}</span>
          <span v-if="c.edited_at" class="ml-1 text-xs text-stone-400">(edited)</span>
        </div>
        <div v-if="editing === c.id" class="mt-1 space-y-2">
          <MarkdownEditor v-model="editBody" :rows="3" label="Edit comment" @submit="save(c)" />
          <div class="flex gap-2">
            <button class="btn-primary" @click="save(c)">Save</button>
            <button class="btn-ghost" @click="editing = ''">Cancel</button>
          </div>
        </div>
        <div v-else class="card-surface mt-1 px-3 py-2"><Markdown :src="c.body" /></div>
        <div v-if="editing !== c.id" class="mt-1 flex gap-3 text-xs text-stone-500">
          <button v-if="c.author.id === session.user?.id && editable" class="hover:underline" @click="(editing = c.id), (editBody = c.body)">Edit</button>
          <button v-if="(c.author.id === session.user?.id && editable) || isAdmin" class="hover:underline" @click="remove(c)">Delete</button>
        </div>
      </div>
    </article>
    <button v-if="cursor" class="btn-ghost" @click="load(true)">Older comments</button>
  </section>
</template>
