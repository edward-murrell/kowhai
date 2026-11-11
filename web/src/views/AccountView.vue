<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, call, type Board, type S, type Token, type Workspace } from '../api/client'
import { ago, shortDate } from '../lib/dates'
import { useSession } from '../stores/session'
import Avatar from '../components/Avatar.vue'

const session = useSession()
const prefs = ref<S['NotificationPrefs']>({})
const tokens = ref<Token[]>([])
const workspaces = ref<Workspace[]>([])
const boards = ref<Board[]>([])
const created = ref('')

const form = ref({ name: '', write: true, limit: 'none', workspace: '', board: '', label: '', expires: '' })

const PREFS: [keyof S['NotificationPrefs'], string][] = [
  ['card_member', 'I am added to a card'],
  ['mention', 'Someone @mentions me'],
  ['comment', 'Someone comments on a card I watch or am on'],
  ['due', 'A card I am on is due soon or overdue'],
  ['invite', 'I am added to a workspace or board'],
  ['recurrence_paused', 'A repeating card I made is paused'],
  ['session_waiting', 'A Claude Code session on my card is waiting for me'],
]

async function load() {
  try {
    ;[prefs.value, tokens.value] = await Promise.all([call(api.GET('/me/notification-prefs')), call(api.GET('/me/tokens'))])
    workspaces.value = (await call(api.GET('/workspaces'))).items
    const all = await Promise.all(workspaces.value.map((w) => call(api.GET('/workspaces/{id}/boards', { params: { path: { id: w.id } } }))))
    boards.value = all.flat()
  } catch (e) {
    session.error(e)
  }
}
onMounted(load)

async function setPref(key: keyof S['NotificationPrefs'], v: boolean) {
  prefs.value = await call(api.PUT('/me/notification-prefs', { body: { ...prefs.value, [key]: v } })).catch((e) => {
    session.error(e)
    return prefs.value
  })
}

async function createToken() {
  const f = form.value
  try {
    const t = await call(
      api.POST('/me/tokens', {
        body: {
          name: f.name,
          scopes: f.write ? ['read', 'write'] : ['read'],
          workspace_id: f.limit === 'workspace' ? f.workspace : undefined,
          board_id: f.limit === 'board' ? f.board : undefined,
          actor_label: f.label || undefined,
          expires_at: f.expires ? new Date(f.expires).toISOString() : undefined,
        },
      }),
    )
    created.value = t.token
    form.value = { name: '', write: true, limit: 'none', workspace: '', board: '', label: '', expires: '' }
    tokens.value = await call(api.GET('/me/tokens'))
  } catch (e) {
    session.error(e)
  }
}

async function revoke(t: Token) {
  if (!confirm(`Revoke ${t.name}? Anything using it stops working.`)) return
  await call(api.DELETE('/me/tokens/{id}', { params: { path: { id: t.id } } })).catch(session.error)
  tokens.value = await call(api.GET('/me/tokens'))
}

function scopeText(t: Token) {
  const where = t.board_id ? `board ${boards.value.find((b) => b.id === t.board_id)?.name ?? t.board_id}` : t.workspace_id ? `workspace ${workspaces.value.find((w) => w.id === t.workspace_id)?.name ?? t.workspace_id}` : 'everything you can see'
  return `${t.scopes.includes('write') ? 'Read and write' : 'Read only'} · ${where}`
}

const active = computed(() => tokens.value.filter((t) => !t.revoked))
async function copyToken() {
  await navigator.clipboard.writeText(created.value)
  session.toast('Copied.')
}
</script>

<template>
  <div class="mx-auto max-w-3xl space-y-8 p-6">
    <section class="card-surface flex items-center gap-4 p-5">
      <Avatar :name="session.user?.display_name" :url="session.user?.avatar_url" :size="56" />
      <div>
        <h1 class="text-xl font-bold">{{ session.user?.display_name }}</h1>
        <p class="text-stone-600">{{ session.user?.email }}</p>
        <p class="mt-1 text-xs text-stone-500">Your name and email come from your sign-in provider.</p>
      </div>
    </section>

    <section>
      <h2 class="mb-2 text-lg font-semibold">Notifications</h2>
      <p class="mb-3 text-sm text-stone-600">Kōwhai shows these under the bell. Turn off the ones you do not want.</p>
      <div class="card-surface divide-y divide-stone-100">
        <label v-for="[key, text] in PREFS" :key="key" class="flex items-center justify-between px-4 py-2.5 text-sm">
          {{ text }}
          <input type="checkbox" :checked="prefs[key] !== false" @change="setPref(key, ($event.target as HTMLInputElement).checked)" />
        </label>
      </div>
    </section>

    <section>
      <h2 class="mb-2 text-lg font-semibold">API tokens</h2>
      <p class="mb-3 text-sm text-stone-600">
        Tokens let scripts, the MCP server and the <code>kowhai hook</code> act as you. Limit each one to what it needs. The actor label shows next to your name on its changes.
      </p>
      <div v-if="created" class="mb-4 rounded-lg border border-kowhai-400 bg-kowhai-50 p-4">
        <p class="text-sm font-semibold">Copy this token now. It is not shown again.</p>
        <div class="mt-2 flex gap-2">
          <code class="flex-1 break-all rounded bg-white px-2 py-1 text-sm" data-testid="new-token">{{ created }}</code>
          <button class="btn-primary" @click="copyToken">Copy</button>
        </div>
      </div>
      <form class="card-surface grid gap-3 p-4 sm:grid-cols-2" @submit.prevent="createToken">
        <label><span class="label-text">Name</span><input v-model="form.name" class="input" required placeholder="e.g. Claude on my laptop" /></label>
        <label><span class="label-text">Actor label</span><input v-model="form.label" class="input" placeholder="e.g. Claude (kowhai repo)" /></label>
        <label>
          <span class="label-text">Can reach</span>
          <select v-model="form.limit" class="input">
            <option value="none">Everything I can see</option>
            <option value="workspace">One workspace</option>
            <option value="board">One board</option>
          </select>
        </label>
        <label v-if="form.limit === 'workspace'">
          <span class="label-text">Workspace</span>
          <select v-model="form.workspace" class="input" required>
            <option v-for="w in workspaces" :key="w.id" :value="w.id">{{ w.name }}</option>
          </select>
        </label>
        <label v-else-if="form.limit === 'board'">
          <span class="label-text">Board</span>
          <select v-model="form.board" class="input" required>
            <option v-for="b in boards" :key="b.id" :value="b.id">{{ b.name }}</option>
          </select>
        </label>
        <div v-else />
        <label><span class="label-text">Expires (optional)</span><input v-model="form.expires" type="date" class="input" /></label>
        <label class="flex items-center gap-2 self-end pb-2 text-sm"><input v-model="form.write" type="checkbox" /> Allow changes (write)</label>
        <div class="sm:col-span-2"><button class="btn-primary">Create token</button></div>
      </form>
      <ul class="card-surface mt-4 divide-y divide-stone-100">
        <li v-if="!active.length" class="px-4 py-4 text-sm text-stone-500">No tokens yet.</li>
        <li v-for="t in active" :key="t.id" class="flex items-center gap-3 px-4 py-3 text-sm">
          <div class="min-w-0 flex-1">
            <div class="font-medium">
              {{ t.name }} <code class="ml-1 text-xs text-stone-500">{{ t.prefix }}…</code>
              <span v-if="t.actor_label" class="ml-1 rounded bg-purple-100 px-1 text-xs text-purple-800">{{ t.actor_label }}</span>
            </div>
            <div class="text-xs text-stone-500">
              {{ scopeText(t) }} · {{ t.last_used_at ? 'used ' + ago(t.last_used_at) : 'never used' }}<template v-if="t.expires_at"> · expires {{ shortDate(t.expires_at) }}</template>
            </div>
          </div>
          <button class="btn-ghost text-red-700" @click="revoke(t)">Revoke</button>
        </li>
      </ul>
    </section>
  </div>
</template>
