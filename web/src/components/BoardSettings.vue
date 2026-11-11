<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { TabsContent, TabsList, TabsRoot, TabsTrigger } from 'reka-ui'
import { api, call, type BoardMember, type Card, type Recurrence, type User } from '../api/client'
import { BACKGROUNDS, boardBackground, LABEL_COLOURS } from '../lib/colours'
import { longDate } from '../lib/dates'
import { useBoard } from '../stores/board'
import { useSession } from '../stores/session'
import Avatar from './Avatar.vue'
import LabelChip from './LabelChip.vue'
import Modal from './Modal.vue'
import UserPicker from './UserPicker.vue'

const open = defineModel<boolean>('open', { default: false })
const board = useBoard()
const session = useSession()
const router = useRouter()

const tab = ref('general')
const name = ref('')
const description = ref('')
const visibility = ref<'workspace' | 'private'>('workspace')
const members = ref<BoardMember[]>([])
const templates = ref<Card[]>([])
const recurrences = ref<Recurrence[]>([])
const mappingOn = ref(false)
const mapping = ref<Record<string, string>>({})

const id = computed(() => board.board!.id)
const admin = computed(() => board.isAdmin)

watch(open, async (v) => {
  if (!v || !board.board) return
  const b = board.board
  name.value = b.name
  description.value = b.description
  visibility.value = b.visibility
  mappingOn.value = !!b.settings.status_lists_enabled
  mapping.value = { ...(b.settings.status_lists ?? {}) }
  try {
    ;[members.value, templates.value, recurrences.value] = await Promise.all([
      call(api.GET('/boards/{id}/members', { params: { path: { id: id.value } } })),
      call(api.GET('/boards/{id}/card-templates', { params: { path: { id: id.value } } })),
      call(api.GET('/boards/{id}/recurrences', { params: { path: { id: id.value } } })),
    ])
  } catch (e) {
    session.error(e)
  }
})

async function patch(body: Record<string, unknown>) {
  try {
    const b = await call(api.PATCH('/boards/{id}', { params: { path: { id: id.value } }, body: body as any }))
    board.board = b
    session.toast('Saved.')
  } catch (e) {
    session.error(e)
  }
}

async function saveLabel(lid: string, body: { name?: string; colour?: string }) {
  await call(api.PATCH('/labels/{id}', { params: { path: { id: lid } }, body })).catch(session.error)
}
async function deleteLabel(lid: string) {
  if (!confirm('Delete this label from every card?')) return
  await call(api.DELETE('/labels/{id}', { params: { path: { id: lid } } })).catch(session.error)
}
async function addLabel() {
  await call(api.POST('/boards/{id}/labels', { params: { path: { id: id.value } }, body: { colour: 'blue', name: 'New label' } })).catch(session.error)
}

async function reloadMembers() {
  members.value = await call(api.GET('/boards/{id}/members', { params: { path: { id: id.value } } }))
}
async function addMember(u: User) {
  await call(api.POST('/boards/{id}/members', { params: { path: { id: id.value } }, body: { user_id: u.id, role: 'editor' } })).catch(session.error)
  await reloadMembers()
}
async function setRole(m: BoardMember, role: string) {
  await call(api.PATCH('/boards/{id}/members/{userId}', { params: { path: { id: id.value, userId: m.user.id } }, body: { role: role as any } })).catch(session.error)
  await reloadMembers()
}
async function removeMember(m: BoardMember) {
  await call(api.DELETE('/boards/{id}/members/{userId}', { params: { path: { id: id.value, userId: m.user.id } } })).catch(session.error)
  await reloadMembers()
}

async function saveMapping() {
  const lists: Record<string, string> = {}
  for (const [k, v] of Object.entries(mapping.value)) if (v) lists[k] = v
  await patch({ settings: { status_lists_enabled: mappingOn.value, status_lists: lists } })
}

async function togglePause(r: Recurrence) {
  try {
    const nr = await call(api.PATCH('/recurrences/{id}', { params: { path: { id: r.id } }, body: { paused: !r.paused } }))
    Object.assign(r, nr)
  } catch (e) {
    session.error(e)
  }
}

function openCard(cid?: string) {
  if (!cid) return
  open.value = false
  router.push({ query: { card: cid } })
}

async function destroy() {
  if (!confirm(`Delete ${board.board?.name} with all its cards? This cannot be undone.`)) return
  const ws = board.board!.workspace_id
  await call(api.DELETE('/boards/{id}', { params: { path: { id: id.value } } }))
    .then(() => router.push(`/w/${ws}`))
    .catch(session.error)
}

const templateTitle = (cid?: string) => templates.value.find((t) => t.id === cid)?.title ?? 'Deleted template'
const origin = location.origin
</script>

<template>
  <Modal v-model:open="open" title="Board settings" wide>
    <TabsRoot v-if="board.board" v-model="tab">
      <TabsList class="mb-4 flex flex-wrap gap-1 border-b border-stone-200">
        <TabsTrigger
          v-for="t in ['general', 'labels', 'members', 'templates', 'repeating', 'claude']"
          :key="t"
          :value="t"
          class="-mb-px border-b-2 border-transparent px-3 py-1.5 text-sm capitalize text-stone-600 data-[state=active]:border-bush-700 data-[state=active]:font-semibold"
          >{{ t === 'claude' ? 'Claude Code' : t }}</TabsTrigger
        >
      </TabsList>

      <TabsContent value="general" class="space-y-4">
        <fieldset :disabled="!admin" class="space-y-4">
          <label class="block"><span class="label-text">Name</span><input v-model="name" class="input" /></label>
          <label class="block"><span class="label-text">Description</span><textarea v-model="description" class="input" rows="3" /></label>
          <label class="block">
            <span class="label-text">Visibility</span>
            <select v-model="visibility" class="input">
              <option value="workspace">Everyone in the workspace</option>
              <option value="private">Only board members and workspace admins</option>
            </select>
          </label>
          <button class="btn-primary" @click="patch({ name, description, visibility })">Save</button>
          <div>
            <span class="label-text">Background</span>
            <div class="flex gap-2">
              <button
                v-for="(_, key) in BACKGROUNDS"
                :key="key"
                class="h-8 w-12 rounded ring-offset-2"
                :class="{ 'ring-2 ring-bush-700': board.board.background === key }"
                :style="{ background: boardBackground(key) }"
                :aria-label="`Background ${key}`"
                @click="patch({ background: key })"
              />
            </div>
          </div>
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" :checked="board.board.is_template" @change="patch({ is_template: !board.board.is_template })" />
            Use this board as a template for new boards
          </label>
          <div class="flex gap-2 border-t border-stone-200 pt-4">
            <button class="btn-ghost" @click="patch({ archived: !board.board.archived })">{{ board.board.archived ? 'Restore board' : 'Archive board' }}</button>
            <button class="btn-danger" @click="destroy">Delete board</button>
          </div>
        </fieldset>
      </TabsContent>

      <TabsContent value="labels" class="space-y-2">
        <div v-for="l in board.labels" :key="l.id" class="flex items-center gap-2">
          <LabelChip :label="l" class="w-24 justify-center" />
          <input
            :value="l.name"
            class="input"
            :disabled="!board.canEdit"
            :aria-label="`Name for ${l.colour} label`"
            @change="saveLabel(l.id, { name: ($event.target as HTMLInputElement).value })"
          />
          <select :value="l.colour" class="input w-28" :disabled="!board.canEdit" aria-label="Colour" @change="saveLabel(l.id, { colour: ($event.target as HTMLSelectElement).value })">
            <option v-for="(_, c) in LABEL_COLOURS" :key="c" :value="c">{{ c }}</option>
          </select>
          <button v-if="board.canEdit" class="btn-ghost text-red-700" @click="deleteLabel(l.id)">Delete</button>
        </div>
        <button v-if="board.canEdit" class="btn-ghost" @click="addLabel">+ Add label</button>
      </TabsContent>

      <TabsContent value="members" class="space-y-4">
        <p class="text-sm text-stone-600">
          Workspace members can already see boards visible to the workspace. Add people here to give a specific role on this board, or to share a private board.
        </p>
        <UserPicker v-if="admin" placeholder="Add a workspace member to this board" @pick="addMember" />
        <ul class="card-surface divide-y divide-stone-100">
          <li v-for="m in members" :key="m.user.id" class="flex items-center gap-3 px-3 py-2">
            <Avatar :name="m.user.display_name" :url="m.user.avatar_url" />
            <span class="flex-1">{{ m.user.display_name }}</span>
            <select v-if="admin" :value="m.role" class="input w-28" @change="setRole(m, ($event.target as HTMLSelectElement).value)">
              <option value="admin">Admin</option>
              <option value="editor">Editor</option>
              <option value="viewer">Viewer</option>
            </select>
            <span v-else class="text-sm capitalize">{{ m.role }}</span>
            <button v-if="admin || m.user.id === session.user?.id" class="btn-ghost" @click="removeMember(m)">Remove</button>
          </li>
        </ul>
      </TabsContent>

      <TabsContent value="templates" class="space-y-2 text-sm">
        <p class="text-stone-600">Card templates are hidden from lists. Use a list's menu to make a card from one. Open any card and choose “Make template” to add one.</p>
        <ul class="card-surface divide-y divide-stone-100">
          <li v-if="!templates.length" class="px-3 py-4 text-stone-500">No card templates yet.</li>
          <li v-for="t in templates" :key="t.id">
            <button class="w-full px-3 py-2 text-left hover:bg-stone-50" @click="openCard(t.id)">{{ t.title }}</button>
          </li>
        </ul>
      </TabsContent>

      <TabsContent value="repeating" class="space-y-2 text-sm">
        <p class="text-stone-600">Open a card template and use the Repeat section to make it repeat.</p>
        <ul class="card-surface divide-y divide-stone-100">
          <li v-if="!recurrences.length" class="px-3 py-4 text-stone-500">No repeating cards.</li>
          <li v-for="r in recurrences" :key="r.id" class="flex items-center gap-3 px-3 py-2">
            <button class="min-w-0 flex-1 text-left" @click="openCard(r.template_card_id)">
              <div class="font-medium">{{ templateTitle(r.template_card_id) }}</div>
              <div class="font-mono text-xs text-stone-500">{{ r.rrule }} · {{ r.timezone }}</div>
            </button>
            <span class="text-xs text-stone-500">{{ r.paused ? 'Paused' : r.next_run_at ? 'Next ' + longDate(r.next_run_at) : 'Finished' }}</span>
            <button v-if="board.canEdit" class="btn-ghost" @click="togglePause(r)">{{ r.paused ? 'Resume' : 'Pause' }}</button>
          </li>
        </ul>
      </TabsContent>

      <TabsContent value="claude" class="space-y-4 text-sm">
        <div class="space-y-3">
          <label class="flex items-center gap-2 font-medium">
            <input v-model="mappingOn" type="checkbox" :disabled="!admin" />
            Move cards when their Claude Code status changes
          </label>
          <div class="grid grid-cols-2 gap-2">
            <template v-for="s in ['working', 'waiting', 'idle', 'stale', 'ended']" :key="s">
              <span class="self-center capitalize">{{ s === 'waiting' ? 'Waiting on you' : s }}</span>
              <select v-model="mapping[s]" class="input" :disabled="!admin || !mappingOn">
                <option value="">Don't move</option>
                <option v-for="l in board.sortedLists" :key="l.id" :value="l.id">{{ l.name }}</option>
              </select>
            </template>
          </div>
          <button v-if="admin" class="btn-primary" @click="saveMapping">Save</button>
        </div>
        <div class="rounded-lg bg-stone-100 p-4">
          <p class="font-semibold">Set up a project</p>
          <ol class="mt-2 list-decimal space-y-1 pl-5">
            <li>Create a token under <RouterLink to="/account" class="text-bush-700 underline">Account settings</RouterLink>, limited to this board, with an actor label such as “Claude”.</li>
            <li>Put it in <code>KOWHAI_TOKEN</code> or <code>~/.config/kowhai/config.toml</code>.</li>
            <li>In the project folder run <code>kowhai link --server {{ origin }}</code> and pick a card.</li>
            <li>Run <code>kowhai link --print-config</code> and add the hooks and MCP server it prints.</li>
          </ol>
        </div>
      </TabsContent>
    </TabsRoot>
  </Modal>
</template>
