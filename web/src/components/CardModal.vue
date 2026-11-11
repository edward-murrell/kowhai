<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { api, call, upload, type Activity, type CardDetail, type CardLink } from '../api/client'
import { actorName, describe } from '../lib/activity'
import { LABEL_COLOURS } from '../lib/colours'
import { ago, dueState, fromLocalInput, longDate, toLocalInput } from '../lib/dates'
import { useBoard } from '../stores/board'
import { useSession } from '../stores/session'
import Avatar from './Avatar.vue'
import Checklists from './Checklists.vue'
import Comments from './Comments.vue'
import LabelChip from './LabelChip.vue'
import Markdown from './Markdown.vue'
import MarkdownEditor from './MarkdownEditor.vue'
import Modal from './Modal.vue'
import RecurrenceEditor from './RecurrenceEditor.vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ cardId: string }>()
const emit = defineEmits<{ close: [] }>()
const board = useBoard()
const session = useSession()

const open = ref(true)
watch(open, (v) => !v && emit('close'))

const card = ref<CardDetail | null>(null)
const error = ref('')
const editingTitle = ref(false)
const title = ref('')
const editingDesc = ref(false)
const desc = ref('')
const tab = ref<'comments' | 'activity'>('comments')
const activity = ref<Activity[]>([])
const newChecklist = ref('')
const newLabelName = ref('')
const newLabelColour = ref('green')
const linkURL = ref('')
const uploading = ref(false)
const fileInput = ref<HTMLInputElement>()

const editable = computed(() => board.canEdit)
const list = computed(() => board.lists.find((l) => l.id === card.value?.list_id))
const tick = computed(() => board.cardTicks[props.cardId] ?? 0)
const sessions = computed(() => (card.value?.links ?? []).filter((l) => l.kind === 'claude_code'))
const urls = computed(() => (card.value?.links ?? []).filter((l) => l.kind === 'url'))
const due = computed(() => dueState(card.value?.due_at, card.value?.due_done ?? false))

async function load() {
  try {
    card.value = await call(api.GET('/cards/{id}', { params: { path: { id: props.cardId } } }))
    error.value = ''
    if (tab.value === 'activity') await loadActivity()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
async function loadActivity() {
  activity.value = (await call(api.GET('/cards/{id}/activity', { params: { path: { id: props.cardId }, query: { limit: 100 } } }))).items
}
onMounted(load)
watch(tick, load)
watch(() => props.cardId, load)
watch(tab, (t) => t === 'activity' && loadActivity())

async function patch(body: Record<string, unknown>) {
  if (!card.value) return
  const c = await board.updateCard(card.value.id, body)
  if (c) await load()
}

function startTitle() {
  if (!editable.value) return
  title.value = card.value!.title
  editingTitle.value = true
}
async function saveTitle() {
  editingTitle.value = false
  if (title.value.trim() && title.value !== card.value?.title) await patch({ title: title.value.trim() })
}
function startDesc() {
  if (!editable.value) return
  desc.value = card.value!.description
  editingDesc.value = true
}
async function saveDesc() {
  editingDesc.value = false
  if (desc.value !== card.value?.description) await patch({ description: desc.value })
}

async function toggleLabel(id: string) {
  const has = card.value!.label_ids.includes(id)
  try {
    if (has) await call(api.DELETE('/cards/{id}/labels/{labelId}', { params: { path: { id: props.cardId, labelId: id } } }))
    else await call(api.POST('/cards/{id}/labels', { params: { path: { id: props.cardId } }, body: { label_id: id } }))
  } catch (e) {
    session.error(e)
  }
  await load()
}
async function createLabel() {
  try {
    const l = await call(api.POST('/boards/{id}/labels', { params: { path: { id: board.board!.id } }, body: { name: newLabelName.value, colour: newLabelColour.value } }))
    newLabelName.value = ''
    await toggleLabel(l.id)
  } catch (e) {
    session.error(e)
  }
}

async function toggleMember(id: string) {
  const has = card.value!.member_ids.includes(id)
  try {
    if (has) await call(api.DELETE('/cards/{id}/members/{userId}', { params: { path: { id: props.cardId, userId: id } } }))
    else await call(api.POST('/cards/{id}/members', { params: { path: { id: props.cardId } }, body: { user_id: id } }))
  } catch (e) {
    session.error(e)
  }
  await load()
}

async function setDue(v: string) {
  await patch({ due_at: fromLocalInput(v) })
}

async function addChecklist() {
  const t = newChecklist.value.trim() || 'Checklist'
  newChecklist.value = ''
  await call(api.POST('/cards/{id}/checklists', { params: { path: { id: props.cardId } }, body: { title: t } })).catch(session.error)
  await load()
}

async function onFiles(files: FileList | null) {
  if (!files?.length) return
  uploading.value = true
  try {
    for (const f of Array.from(files)) await upload(props.cardId, f)
  } catch (e) {
    session.error(e)
  } finally {
    uploading.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
  await load()
}
async function removeAttachment(id: string) {
  if (!confirm('Delete this attachment?')) return
  await call(api.DELETE('/attachments/{id}', { params: { path: { id } } })).catch(session.error)
  await load()
}

async function addLink() {
  const url = linkURL.value.trim()
  if (!/^https?:\/\//.test(url)) {
    session.toast('Links must start with http:// or https://', 'error')
    return
  }
  await call(api.POST('/cards/{id}/links', { params: { path: { id: props.cardId } }, body: { kind: 'url', external_key: url } })).catch(session.error)
  linkURL.value = ''
  await load()
}
async function removeLink(l: CardLink) {
  if (!confirm('Remove this link?')) return
  await call(api.DELETE('/links/{id}', { params: { path: { id: l.id } } })).catch(session.error)
  await load()
}

async function setWatching(on: boolean) {
  await call(on ? api.POST('/cards/{id}/watch', { params: { path: { id: props.cardId } } }) : api.DELETE('/cards/{id}/watch', { params: { path: { id: props.cardId } } })).catch(session.error)
  await load()
}

async function move(listId: string) {
  await board.moveCard(props.cardId, listId, board.cardsIn(listId).length)
  await load()
}

async function copy() {
  try {
    const c = await call(api.POST('/cards/{id}/copy', { params: { path: { id: props.cardId } }, body: { list_id: card.value!.list_id, after_id: props.cardId, as_template: false } }))
    session.toast(`Copied as “${c.title}”.`)
  } catch (e) {
    session.error(e)
  }
}

async function destroy() {
  if (!confirm('Delete this card for good? Archive it instead if you might want it back.')) return
  try {
    await call(api.DELETE('/cards/{id}', { params: { path: { id: props.cardId } } }))
    board.cards = board.cards.filter((c) => c.id !== props.cardId)
    open.value = false
  } catch (e) {
    session.error(e)
  }
}

function sessionHistory(l: CardLink): Activity[] {
  return activity.value.filter((a) => a.type === 'link.status' && (a.payload as any).entity?.id === l.id)
}
</script>

<template>
  <Modal v-model:open="open" title="Card" wide>
    <template #title>
      <span v-if="list" class="text-sm font-normal text-stone-500">in <strong>{{ list.name }}</strong></span>
    </template>
    <p v-if="error" class="text-red-700">{{ error }}</p>
    <div v-else-if="card" class="grid gap-6 md:grid-cols-[1fr_14rem]">
      <div class="min-w-0 space-y-6">
        <div>
          <div v-if="card.archived" class="mb-2 rounded bg-amber-100 px-3 py-2 text-sm text-amber-900">This card is archived.</div>
          <div v-if="card.is_template" class="mb-2 rounded bg-purple-100 px-3 py-2 text-sm text-purple-900">This is a card template. It is hidden from the board.</div>
          <textarea
            v-if="editingTitle"
            v-model="title"
            rows="2"
            class="input text-xl font-bold"
            autofocus
            aria-label="Card title"
            @keydown.enter.prevent="saveTitle"
            @blur="saveTitle"
          />
          <h2 v-else class="cursor-text text-xl font-bold" data-testid="card-title" @click="startTitle">{{ card.title }}</h2>
        </div>

        <div class="flex flex-wrap gap-6">
          <div v-if="card.member_ids.length">
            <span class="label-text">Members</span>
            <div class="flex gap-1">
              <Avatar v-for="id in card.member_ids" :key="id" :name="board.userById(id)?.display_name" :url="board.userById(id)?.avatar_url" />
            </div>
          </div>
          <div v-if="card.label_ids.length">
            <span class="label-text">Labels</span>
            <div class="flex flex-wrap gap-1">
              <template v-for="id in card.label_ids" :key="id">
                <LabelChip v-if="board.labelById(id)" :label="board.labelById(id)!" />
              </template>
            </div>
          </div>
          <div v-if="card.due_at">
            <span class="label-text">Due</span>
            <label class="flex items-center gap-2 text-sm">
              <input type="checkbox" :checked="card.due_done" :disabled="!editable" @change="patch({ due_done: !card.due_done })" />
              <span
                class="rounded px-1.5 py-0.5"
                :class="{ 'bg-red-100 text-red-800': due === 'overdue', 'bg-amber-100 text-amber-900': due === 'soon', 'bg-emerald-100 text-emerald-800': due === 'done' }"
                >{{ longDate(card.due_at) }}</span
              >
            </label>
          </div>
        </div>

        <section>
          <div class="mb-2 flex items-center justify-between">
            <h3 class="font-semibold">Description</h3>
            <button v-if="editable && !editingDesc && card.description" class="btn-ghost text-xs" @click="startDesc">Edit</button>
          </div>
          <div v-if="editingDesc" class="space-y-2">
            <MarkdownEditor v-model="desc" :rows="8" label="Description" @submit="saveDesc" />
            <div class="flex gap-2">
              <button class="btn-primary" @click="saveDesc">Save</button>
              <button class="btn-ghost" @click="editingDesc = false">Cancel</button>
            </div>
          </div>
          <Markdown v-else-if="card.description" :src="card.description" />
          <button v-else-if="editable" class="w-full rounded-md bg-stone-200/70 px-3 py-4 text-left text-sm text-stone-600 hover:bg-stone-200" @click="startDesc">
            Add a more detailed description…
          </button>
        </section>

        <section v-if="sessions.length" class="space-y-2">
          <h3 class="font-semibold">Claude Code sessions</h3>
          <div v-for="l in sessions" :key="l.id" class="card-surface p-3 text-sm">
            <div class="flex flex-wrap items-center gap-2">
              <StatusBadge :link="l" full />
              <span class="text-stone-500">{{ [l.repo, l.host].filter(Boolean).join(' on ') }}</span>
              <span class="ml-auto font-mono text-xs text-stone-400" :title="l.external_key">{{ l.external_key.slice(0, 8) }}</span>
              <button v-if="editable" class="text-xs text-stone-400 hover:text-red-600" aria-label="Remove session" @click="removeLink(l)">✕</button>
            </div>
            <p v-if="l.status === 'waiting' && l.status_detail" class="mt-2 rounded bg-amber-50 px-2 py-1 text-amber-900">{{ l.status_detail }}</p>
            <details class="mt-2" @toggle="tab === 'activity' || loadActivity()">
              <summary class="cursor-pointer text-xs text-stone-500">History</summary>
              <ul class="mt-1 space-y-0.5 text-xs text-stone-600">
                <li v-for="a in sessionHistory(l)" :key="a.id">{{ ago(a.created_at) }} — {{ (a.payload as any).status }}<template v-if="(a.payload as any).detail">: {{ (a.payload as any).detail }}</template></li>
              </ul>
            </details>
          </div>
        </section>

        <Checklists v-if="card.checklists.length" :checklists="card.checklists" :editable="editable" @changed="load" />

        <section v-if="card.attachments.length" class="space-y-2">
          <h3 class="font-semibold">Attachments</h3>
          <ul class="space-y-2">
            <li v-for="a in card.attachments" :key="a.id" class="flex items-center gap-3 text-sm">
              <a :href="a.url" target="_blank" rel="noopener" class="flex h-14 w-20 shrink-0 items-center justify-center overflow-hidden rounded bg-stone-200">
                <img v-if="a.content_type.startsWith('image/') && a.content_type !== 'image/svg+xml'" :src="a.url" alt="" class="h-full w-full object-cover" />
                <span v-else class="text-xs font-semibold uppercase text-stone-500">{{ a.filename.split('.').pop() }}</span>
              </a>
              <div class="min-w-0 flex-1">
                <a :href="a.url + '?download=1'" class="block truncate font-medium hover:underline">{{ a.filename }}</a>
                <div class="text-xs text-stone-500">
                  {{ (a.size_bytes / 1024).toFixed(0) }} KB · {{ ago(a.created_at) }}
                  <template v-if="editable">
                    ·
                    <button v-if="card.cover_attachment_id !== a.id && a.content_type.startsWith('image/')" class="hover:underline" @click="patch({ cover_attachment_id: a.id })">Make cover</button>
                    <button v-else-if="card.cover_attachment_id === a.id" class="hover:underline" @click="patch({ cover_attachment_id: null })">Remove cover</button>
                    · <button class="hover:underline" @click="removeAttachment(a.id)">Delete</button>
                  </template>
                </div>
              </div>
            </li>
          </ul>
        </section>

        <section v-if="urls.length" class="space-y-1">
          <h3 class="font-semibold">Links</h3>
          <div v-for="l in urls" :key="l.id" class="flex items-center gap-2 text-sm">
            <a :href="l.external_key" target="_blank" rel="noopener noreferrer" class="truncate text-bush-700 hover:underline">{{ l.title || l.external_key }}</a>
            <button v-if="editable" class="text-xs text-stone-400 hover:text-red-600" aria-label="Remove link" @click="removeLink(l)">✕</button>
          </div>
        </section>

        <section v-if="card.is_template && editable" class="space-y-2">
          <h3 class="font-semibold">Repeat</h3>
          <RecurrenceEditor :card-id="card.id" :board-id="card.board_id" />
        </section>

        <section>
          <div class="mb-3 flex gap-1 border-b border-stone-200">
            <button class="-mb-px border-b-2 px-3 py-1.5 text-sm" :class="tab === 'comments' ? 'border-bush-700 font-semibold' : 'border-transparent text-stone-500'" @click="tab = 'comments'">Comments</button>
            <button class="-mb-px border-b-2 px-3 py-1.5 text-sm" :class="tab === 'activity' ? 'border-bush-700 font-semibold' : 'border-transparent text-stone-500'" @click="tab = 'activity'">Activity</button>
          </div>
          <Comments v-if="tab === 'comments'" :card-id="card.id" :tick="tick" :editable="editable" :is-admin="board.isAdmin" />
          <ul v-else class="space-y-2">
            <li v-for="a in activity" :key="a.id" class="flex gap-2 text-sm">
              <Avatar :name="actorName(a)" :url="a.actor?.avatar_url" :size="24" />
              <div><span class="font-semibold">{{ actorName(a) }}</span> {{ describe(a) }} <span class="text-xs text-stone-500">{{ ago(a.created_at) }}</span></div>
            </li>
          </ul>
        </section>
      </div>

      <aside class="space-y-4 text-sm">
        <div v-if="editable" class="space-y-1.5">
          <span class="label-text">Add to card</span>
          <PopoverRoot>
            <PopoverTrigger class="btn-ghost w-full justify-start bg-stone-200/70">Members</PopoverTrigger>
            <PopoverPortal>
              <PopoverContent class="menu w-64" align="start" :side-offset="4">
                <button v-for="u in board.users" :key="u.id" class="menu-item w-full" @click="toggleMember(u.id)">
                  <Avatar :name="u.display_name" :url="u.avatar_url" :size="22" />
                  <span class="flex-1 text-left">{{ u.display_name }}</span>
                  <span v-if="card.member_ids.includes(u.id)">✓</span>
                </button>
              </PopoverContent>
            </PopoverPortal>
          </PopoverRoot>
          <PopoverRoot>
            <PopoverTrigger class="btn-ghost w-full justify-start bg-stone-200/70">Labels</PopoverTrigger>
            <PopoverPortal>
              <PopoverContent class="menu w-64 space-y-1 p-2" align="start" :side-offset="4">
                <button v-for="l in board.labels" :key="l.id" class="flex w-full items-center gap-2" @click="toggleLabel(l.id)">
                  <LabelChip :label="l" class="flex-1" />
                  <span class="w-4">{{ card.label_ids.includes(l.id) ? '✓' : '' }}</span>
                </button>
                <form class="space-y-1 border-t border-stone-200 pt-2" @submit.prevent="createLabel">
                  <input v-model="newLabelName" class="input" placeholder="New label name" aria-label="New label name" />
                  <div class="flex flex-wrap gap-1">
                    <button
                      v-for="(c, key) in LABEL_COLOURS"
                      :key="key"
                      type="button"
                      class="h-5 w-7 rounded"
                      :class="{ 'ring-2 ring-stone-900': newLabelColour === key }"
                      :style="{ backgroundColor: c.bg }"
                      :aria-label="`Colour ${key}`"
                      @click="newLabelColour = key"
                    />
                  </div>
                  <button class="btn-primary w-full justify-center">Create label</button>
                </form>
              </PopoverContent>
            </PopoverPortal>
          </PopoverRoot>
          <PopoverRoot>
            <PopoverTrigger class="btn-ghost w-full justify-start bg-stone-200/70">Due date</PopoverTrigger>
            <PopoverPortal>
              <PopoverContent class="menu w-64 space-y-2 p-3" align="start" :side-offset="4">
                <input type="datetime-local" class="input" :value="toLocalInput(card.due_at)" aria-label="Due date" @change="setDue(($event.target as HTMLInputElement).value)" />
                <button v-if="card.due_at" class="btn-ghost w-full justify-center" @click="setDue('')">Remove due date</button>
              </PopoverContent>
            </PopoverPortal>
          </PopoverRoot>
          <PopoverRoot>
            <PopoverTrigger class="btn-ghost w-full justify-start bg-stone-200/70">Checklist</PopoverTrigger>
            <PopoverPortal>
              <PopoverContent class="menu w-64 p-3" align="start" :side-offset="4">
                <form class="space-y-2" @submit.prevent="addChecklist">
                  <input v-model="newChecklist" class="input" placeholder="Checklist" aria-label="Checklist title" />
                  <button class="btn-primary w-full justify-center">Add checklist</button>
                </form>
              </PopoverContent>
            </PopoverPortal>
          </PopoverRoot>
          <button class="btn-ghost w-full justify-start bg-stone-200/70" :disabled="uploading" @click="fileInput?.click()">
            {{ uploading ? 'Uploading…' : 'Attachment' }}
          </button>
          <input ref="fileInput" type="file" multiple class="hidden" data-testid="file-input" @change="onFiles(($event.target as HTMLInputElement).files)" />
          <PopoverRoot>
            <PopoverTrigger class="btn-ghost w-full justify-start bg-stone-200/70">Link</PopoverTrigger>
            <PopoverPortal>
              <PopoverContent class="menu w-72 p-3" align="start" :side-offset="4">
                <form class="space-y-2" @submit.prevent="addLink">
                  <input v-model="linkURL" type="url" class="input" placeholder="https://…" aria-label="Link URL" />
                  <button class="btn-primary w-full justify-center">Add link</button>
                </form>
              </PopoverContent>
            </PopoverPortal>
          </PopoverRoot>
        </div>
        <div class="space-y-1.5">
          <span class="label-text">Actions</span>
          <button class="btn-ghost w-full justify-start bg-stone-200/70" @click="setWatching(!card.watching)">{{ card.watching ? 'Stop watching' : 'Watch' }}</button>
          <template v-if="editable">
            <label class="block">
              <span class="sr-only">Move to list</span>
              <select class="input" :value="card.list_id" aria-label="Move to list" @change="move(($event.target as HTMLSelectElement).value)">
                <option v-for="l in board.sortedLists" :key="l.id" :value="l.id">Move to {{ l.name }}</option>
              </select>
            </label>
            <button class="btn-ghost w-full justify-start bg-stone-200/70" @click="copy">Copy</button>
            <button class="btn-ghost w-full justify-start bg-stone-200/70" @click="patch({ is_template: !card.is_template })">
              {{ card.is_template ? 'Turn into a normal card' : 'Make template' }}
            </button>
            <button class="btn-ghost w-full justify-start bg-stone-200/70" @click="patch({ archived: !card.archived })">{{ card.archived ? 'Restore' : 'Archive' }}</button>
            <button v-if="card.archived || card.is_template" class="btn-danger w-full justify-start" @click="destroy">Delete</button>
          </template>
        </div>
        <p class="text-xs text-stone-500">Created {{ ago(card.created_at) }} · version {{ card.version }}</p>
      </aside>
    </div>
    <p v-else class="text-stone-500">Loading…</p>
  </Modal>
</template>
