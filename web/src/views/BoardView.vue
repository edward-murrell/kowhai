<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { VueDraggable } from 'vue-draggable-plus'
import { api, call, type Card, type List } from '../api/client'
import { boardBackground } from '../lib/colours'
import { dueState } from '../lib/dates'
import { useBoard } from '../stores/board'
import { useSession } from '../stores/session'
import ActivityFeed from '../components/ActivityFeed.vue'
import BoardSettings from '../components/BoardSettings.vue'
import CardModal from '../components/CardModal.vue'
import LabelChip from '../components/LabelChip.vue'
import ListColumn from '../components/ListColumn.vue'

const props = defineProps<{ id: string }>()
const board = useBoard()
const session = useSession()
const route = useRoute()
const router = useRouter()

const loading = ref(true)
const error = ref('')
const settingsOpen = ref(false)
const activityOpen = ref(false)
const addingList = ref(false)
const listName = ref('')
const listInput = ref<HTMLInputElement>()
const editingName = ref(false)
const boardName = ref('')

// Filters.
const text = ref('')
const labelFilter = ref<string[]>([])
const memberFilter = ref('')
const dueFilter = ref('')
const filtering = computed(() => !!(text.value || labelFilter.value.length || memberFilter.value || dueFilter.value))

function matches(c: Card): boolean {
  if (text.value && !(c.title + ' ' + c.description).toLowerCase().includes(text.value.toLowerCase())) return false
  if (labelFilter.value.length && !labelFilter.value.some((l) => c.label_ids.includes(l))) return false
  if (memberFilter.value === 'me' && !c.member_ids.includes(session.user?.id ?? '')) return false
  if (memberFilter.value && memberFilter.value !== 'me' && !c.member_ids.includes(memberFilter.value)) return false
  if (dueFilter.value) {
    const s = dueState(c.due_at, c.due_done)
    if (dueFilter.value === 'overdue' && s !== 'overdue') return false
    if (dueFilter.value === 'soon' && s !== 'soon' && s !== 'overdue') return false
    if (dueFilter.value === 'none' && c.due_at) return false
  }
  return true
}

function clearFilters() {
  text.value = ''
  labelFilter.value = []
  memberFilter.value = ''
  dueFilter.value = ''
}

const openCard = computed(() => (typeof route.query.card === 'string' ? route.query.card : ''))
function open(id: string) {
  router.push({ query: { ...route.query, card: id } })
}
function closeCard() {
  const q = { ...route.query }
  delete q.card
  router.push({ query: q })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    await board.load(props.id)
    document.title = `${board.board?.name} · Kōwhai`
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(() => props.id, load, { immediate: true })
watch(
  () => board.showArchived,
  () => board.reload(),
)
onBeforeUnmount(() => {
  board.close()
  document.title = 'Kōwhai'
})

// Lists are dragged by their header.
const lists = ref<List[]>([])
watch(
  () => board.sortedLists,
  (v) => (lists.value = [...v]),
  { immediate: true, deep: true },
)
function onListEnd(evt: { item: HTMLElement; newIndex?: number; oldIndex?: number }) {
  const id = evt.item.dataset.listId
  if (id && evt.newIndex !== undefined && evt.newIndex !== evt.oldIndex) void board.moveList(id, evt.newIndex)
}

async function startList() {
  addingList.value = true
  await nextTick()
  listInput.value?.focus()
}
async function addList() {
  const n = listName.value.trim()
  if (!n) return
  listName.value = ''
  await board.addList(n)
}

function startRename() {
  if (!board.isAdmin) return
  boardName.value = board.board?.name ?? ''
  editingName.value = true
}
async function renameBoard() {
  editingName.value = false
  const b = board.board
  if (!b || !boardName.value.trim() || boardName.value === b.name) return
  try {
    board.board = { ...(await call(api.PATCH('/boards/{id}', { params: { path: { id: b.id } }, body: { name: boardName.value.trim() } }))) }
  } catch (e) {
    session.error(e)
  }
}
</script>

<template>
  <div v-if="error" class="p-8 text-center">
    <p class="font-semibold">This board could not be opened.</p>
    <p class="mt-1 text-sm text-stone-600">{{ error }}</p>
    <RouterLink to="/" class="btn-primary mt-4">Back to workspaces</RouterLink>
  </div>
  <div v-else-if="board.board" class="flex h-full flex-col" :style="{ background: boardBackground(board.board.background) }">
    <div class="flex flex-wrap items-center gap-2 bg-black/25 px-4 py-2 text-white backdrop-blur-sm">
      <input
        v-if="editingName"
        v-model="boardName"
        class="rounded bg-white px-2 py-1 text-lg font-bold text-stone-900"
        autofocus
        aria-label="Board name"
        @keydown.enter="renameBoard"
        @blur="renameBoard"
      />
      <h1 v-else class="cursor-text rounded px-2 py-1 text-lg font-bold hover:bg-white/15" @click="startRename">{{ board.board.name }}</h1>
      <RouterLink :to="`/w/${board.board.workspace_id}`" class="text-sm text-white/80 hover:underline">Workspace</RouterLink>
      <span v-if="board.board.visibility === 'private'" class="rounded bg-white/20 px-1.5 py-0.5 text-xs">Private</span>
      <span v-if="board.board.role === 'viewer'" class="rounded bg-white/20 px-1.5 py-0.5 text-xs">View only</span>
      <span v-if="board.board.archived" class="rounded bg-red-500/80 px-1.5 py-0.5 text-xs">Archived</span>
      <span
        class="h-2 w-2 rounded-full"
        :class="board.connected ? 'bg-emerald-400' : 'bg-stone-400'"
        :title="board.connected ? 'Live updates on' : 'Reconnecting…'"
        data-testid="live-indicator"
      />
      <div class="ml-auto flex flex-wrap items-center gap-2">
        <input v-model="text" type="search" placeholder="Filter cards" class="w-40 rounded bg-white/90 px-2 py-1 text-sm text-stone-900" aria-label="Filter cards" />
        <select v-model="memberFilter" class="rounded bg-white/90 px-1.5 py-1 text-sm text-stone-900" aria-label="Filter by member">
          <option value="">Anyone</option>
          <option value="me">My cards</option>
          <option v-for="u in board.users" :key="u.id" :value="u.id">{{ u.display_name }}</option>
        </select>
        <select v-model="dueFilter" class="rounded bg-white/90 px-1.5 py-1 text-sm text-stone-900" aria-label="Filter by due date">
          <option value="">Any due date</option>
          <option value="overdue">Overdue</option>
          <option value="soon">Due in 24 hours</option>
          <option value="none">No due date</option>
        </select>
        <div class="flex gap-1">
          <button
            v-for="l in board.labels"
            :key="l.id"
            class="rounded"
            :class="labelFilter.includes(l.id) ? 'ring-2 ring-white' : 'opacity-70 hover:opacity-100'"
            :aria-pressed="labelFilter.includes(l.id)"
            :aria-label="`Filter label ${l.name || l.colour}`"
            @click="labelFilter = labelFilter.includes(l.id) ? labelFilter.filter((x) => x !== l.id) : [...labelFilter, l.id]"
          >
            <LabelChip :label="l" compact />
          </button>
        </div>
        <button v-if="filtering" class="rounded bg-white/20 px-2 py-1 text-sm hover:bg-white/30" @click="clearFilters">Clear</button>
        <label class="flex items-center gap-1 text-sm"><input v-model="board.showArchived" type="checkbox" /> Archived</label>
        <button class="rounded bg-white/20 px-2 py-1 text-sm hover:bg-white/30" @click="activityOpen = !activityOpen">Activity</button>
        <button class="rounded bg-white/20 px-2 py-1 text-sm hover:bg-white/30" @click="settingsOpen = true">Settings</button>
      </div>
    </div>
    <div class="flex min-h-0 flex-1">
      <div class="flex min-h-0 flex-1 items-start gap-3 overflow-x-auto p-4">
        <VueDraggable
          v-model="lists"
          :animation="150"
          handle=".list-handle"
          :disabled="!board.canEdit"
          class="flex h-full items-start gap-3"
          @end="onListEnd"
        >
          <ListColumn v-for="l in lists" :key="l.id" :list="l" :filter="matches" @open="open" />
        </VueDraggable>
        <div v-if="board.canEdit" class="w-72 shrink-0">
          <form v-if="addingList" class="space-y-2 rounded-xl bg-stone-100 p-2" @submit.prevent="addList">
            <input ref="listInput" v-model="listName" class="input" placeholder="List name" aria-label="New list name" @keydown.esc="addingList = false" />
            <div class="flex gap-2">
              <button class="btn-primary">Add list</button>
              <button type="button" class="btn-ghost" @click="addingList = false">Cancel</button>
            </div>
          </form>
          <button v-else class="w-full rounded-xl bg-white/25 px-3 py-2.5 text-left text-sm font-medium text-white hover:bg-white/35" @click="startList">
            + Add a list
          </button>
        </div>
      </div>
      <ActivityFeed v-if="activityOpen" class="w-80 shrink-0" @close="activityOpen = false" @open="open" />
    </div>
    <CardModal v-if="openCard" :card-id="openCard" @close="closeCard" />
    <BoardSettings v-model:open="settingsOpen" />
  </div>
  <div v-else-if="loading" class="p-8 text-center text-stone-500">Loading board…</div>
</template>
