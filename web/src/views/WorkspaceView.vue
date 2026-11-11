<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { TabsContent, TabsList, TabsRoot, TabsTrigger } from 'reka-ui'
import { api, call, type Board, type S, type User, type Workspace, type WorkspaceMember } from '../api/client'
import { boardBackground } from '../lib/colours'
import { useSession } from '../stores/session'
import Avatar from '../components/Avatar.vue'
import NewBoardDialog from '../components/NewBoardDialog.vue'
import UserPicker from '../components/UserPicker.vue'

const props = defineProps<{ id: string }>()
const session = useSession()
const route = useRoute()
const router = useRouter()

const ws = ref<Workspace | null>(null)
const boards = ref<Board[]>([])
const archived = ref<Board[]>([])
const members = ref<WorkspaceMember[]>([])
const invites = ref<S['WorkspaceInvite'][]>([])
const newBoard = ref(route.query.new === '1')
const tab = ref('boards')
const inviteEmail = ref('')
const inviteRole = ref<'member' | 'admin' | 'viewer'>('member')
const name = ref('')
const description = ref('')

const isAdmin = computed(() => ws.value?.role === 'owner' || ws.value?.role === 'admin')
const canCreate = computed(() => ws.value && ws.value.role !== 'viewer')

async function load() {
  const id = props.id
  try {
    ws.value = await call(api.GET('/workspaces/{id}', { params: { path: { id } } }))
    name.value = ws.value.name
    description.value = ws.value.description
    const [b, all, m] = await Promise.all([
      call(api.GET('/workspaces/{id}/boards', { params: { path: { id } } })),
      call(api.GET('/workspaces/{id}/boards', { params: { path: { id }, query: { archived: true } } })),
      call(api.GET('/workspaces/{id}/members', { params: { path: { id } } })),
    ])
    boards.value = b
    archived.value = all.filter((x) => x.archived)
    members.value = m
    if (isAdmin.value) invites.value = await call(api.GET('/workspaces/{id}/invites', { params: { path: { id } } }))
  } catch (e) {
    session.error(e)
  }
}

async function addMember(u: User) {
  try {
    await call(api.POST('/workspaces/{id}/members', { params: { path: { id: props.id } }, body: { user_id: u.id, role: 'member' } }))
    await load()
  } catch (e) {
    session.error(e)
  }
}

async function setRole(m: WorkspaceMember, role: string) {
  try {
    await call(
      api.PATCH('/workspaces/{id}/members/{userId}', { params: { path: { id: props.id, userId: m.user.id } }, body: { role: role as any } }),
    )
    await load()
  } catch (e) {
    session.error(e)
    await load()
  }
}

async function remove(m: WorkspaceMember) {
  const self = m.user.id === session.user?.id
  if (!confirm(self ? 'Leave this workspace?' : `Remove ${m.user.display_name}?`)) return
  try {
    await call(api.DELETE('/workspaces/{id}/members/{userId}', { params: { path: { id: props.id, userId: m.user.id } } }))
    if (self) router.push('/')
    else await load()
  } catch (e) {
    session.error(e)
  }
}

async function invite() {
  try {
    const r = await call(
      api.POST('/workspaces/{id}/invites', { params: { path: { id: props.id } }, body: { email: inviteEmail.value, role: inviteRole.value } }),
    )
    session.toast(r.member ? `${r.member.user.display_name} added.` : `Invite saved. ${inviteEmail.value} joins on first sign-in.`)
    inviteEmail.value = ''
    await load()
  } catch (e) {
    session.error(e)
  }
}

async function cancelInvite(id: string) {
  await call(api.DELETE('/workspaces/{id}/invites/{inviteId}', { params: { path: { id: props.id, inviteId: id } } })).catch(session.error)
  await load()
}

async function save() {
  try {
    ws.value = await call(
      api.PATCH('/workspaces/{id}', { params: { path: { id: props.id } }, body: { name: name.value, description: description.value } }),
    )
    session.toast('Saved.')
  } catch (e) {
    session.error(e)
  }
}

async function destroy() {
  if (!confirm(`Delete ${ws.value?.name} and every board in it? This cannot be undone.`)) return
  try {
    await call(api.DELETE('/workspaces/{id}', { params: { path: { id: props.id } } }))
    router.push('/')
  } catch (e) {
    session.error(e)
  }
}

onMounted(load)
watch(() => props.id, load)
</script>

<template>
  <div v-if="ws" class="mx-auto max-w-6xl p-6">
    <div class="mb-4">
      <h1 class="text-2xl font-bold">{{ ws.name }}</h1>
      <p v-if="ws.description" class="mt-1 text-stone-600">{{ ws.description }}</p>
    </div>
    <TabsRoot v-model="tab">
      <TabsList class="mb-5 flex gap-1 border-b border-stone-300">
        <TabsTrigger
          v-for="t in ['boards', 'members', ...(isAdmin ? ['settings'] : [])]"
          :key="t"
          :value="t"
          class="-mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium capitalize text-stone-600 data-[state=active]:border-bush-700 data-[state=active]:text-bush-800"
          >{{ t }}</TabsTrigger
        >
      </TabsList>

      <TabsContent value="boards">
        <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
          <RouterLink
            v-for="b in boards"
            :key="b.id"
            :to="`/b/${b.id}`"
            class="flex h-24 flex-col justify-between rounded-lg p-3 text-white shadow-sm transition-transform hover:-translate-y-0.5"
            :style="{ background: boardBackground(b.background) }"
          >
            <span class="font-semibold drop-shadow">{{ b.name }}</span>
            <span class="text-xs opacity-80">{{ b.visibility === 'private' ? 'Private' : '' }}</span>
          </RouterLink>
          <button
            v-if="canCreate"
            class="flex h-24 items-center justify-center rounded-lg border-2 border-dashed border-stone-300 text-sm text-stone-500 hover:border-stone-400 hover:bg-white"
            @click="newBoard = true"
          >
            New board
          </button>
        </div>
        <details v-if="archived.length" class="mt-6">
          <summary class="cursor-pointer text-sm text-stone-600">Archived boards ({{ archived.length }})</summary>
          <ul class="mt-2 space-y-1">
            <li v-for="b in archived" :key="b.id">
              <RouterLink :to="`/b/${b.id}`" class="text-sm text-bush-700 hover:underline">{{ b.name }}</RouterLink>
            </li>
          </ul>
        </details>
      </TabsContent>

      <TabsContent value="members" class="space-y-6">
        <div v-if="isAdmin" class="card-surface grid gap-4 p-4 md:grid-cols-2">
          <div>
            <span class="label-text">Add someone who has signed in before</span>
            <UserPicker @pick="addMember" />
          </div>
          <form class="flex items-end gap-2" @submit.prevent="invite">
            <label class="flex-1">
              <span class="label-text">Invite by email</span>
              <input v-model="inviteEmail" type="email" class="input" required placeholder="name@example.com" />
            </label>
            <select v-model="inviteRole" class="input w-28" aria-label="Role">
              <option value="member">Member</option>
              <option value="admin">Admin</option>
              <option value="viewer">Viewer</option>
            </select>
            <button class="btn-primary">Invite</button>
          </form>
        </div>
        <ul class="card-surface divide-y divide-stone-100">
          <li v-for="m in members" :key="m.user.id" class="flex items-center gap-3 px-4 py-3">
            <Avatar :name="m.user.display_name" :url="m.user.avatar_url" />
            <div class="min-w-0 flex-1">
              <div class="font-medium">{{ m.user.display_name }}</div>
              <div class="text-sm text-stone-500">{{ m.user.email }}</div>
            </div>
            <select
              v-if="isAdmin"
              :value="m.role"
              class="input w-28"
              :aria-label="`Role for ${m.user.display_name}`"
              @change="setRole(m, ($event.target as HTMLSelectElement).value)"
            >
              <option value="owner">Owner</option>
              <option value="admin">Admin</option>
              <option value="member">Member</option>
              <option value="viewer">Viewer</option>
            </select>
            <span v-else class="text-sm capitalize text-stone-600">{{ m.role }}</span>
            <button v-if="isAdmin || m.user.id === session.user?.id" class="btn-ghost text-red-700" @click="remove(m)">
              {{ m.user.id === session.user?.id ? 'Leave' : 'Remove' }}
            </button>
          </li>
        </ul>
        <div v-if="invites.length">
          <h2 class="mb-2 font-semibold">Pending invites</h2>
          <ul class="card-surface divide-y divide-stone-100">
            <li v-for="i in invites" :key="i.id" class="flex items-center gap-3 px-4 py-2 text-sm">
              <span class="flex-1">{{ i.email }}</span>
              <span class="capitalize text-stone-500">{{ i.role }}</span>
              <button class="btn-ghost" @click="cancelInvite(i.id)">Cancel</button>
            </li>
          </ul>
        </div>
      </TabsContent>

      <TabsContent value="settings">
        <form class="card-surface max-w-lg space-y-4 p-4" @submit.prevent="save">
          <label class="block"><span class="label-text">Name</span><input v-model="name" class="input" required /></label>
          <label class="block"
            ><span class="label-text">Description</span><textarea v-model="description" class="input" rows="3"
          /></label>
          <button class="btn-primary">Save</button>
        </form>
        <div v-if="ws.role === 'owner'" class="mt-6 max-w-lg rounded-lg border border-red-200 bg-red-50 p-4">
          <p class="font-semibold text-red-800">Delete workspace</p>
          <p class="mt-1 text-sm text-red-700">Deletes every board, card and attachment in it.</p>
          <button class="btn-danger mt-3" @click="destroy">Delete workspace</button>
        </div>
      </TabsContent>
    </TabsRoot>
    <NewBoardDialog v-model:open="newBoard" :workspace-id="id" />
  </div>
</template>
