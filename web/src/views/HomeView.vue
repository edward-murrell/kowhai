<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, call, type Board, type Workspace } from '../api/client'
import { boardBackground } from '../lib/colours'
import { useSession } from '../stores/session'
import Modal from '../components/Modal.vue'

const session = useSession()
const router = useRouter()
const workspaces = ref<Workspace[]>([])
const boards = ref<Record<string, Board[]>>({})
const loading = ref(true)
const creating = ref(false)
const name = ref('')

async function load() {
  try {
    const page = await call(api.GET('/workspaces'))
    workspaces.value = page.items
    await Promise.all(
      page.items.map(async (w) => {
        boards.value[w.id] = await call(api.GET('/workspaces/{id}/boards', { params: { path: { id: w.id } } }))
      }),
    )
  } catch (e) {
    session.error(e)
  } finally {
    loading.value = false
  }
}

async function create() {
  try {
    const w = await call(api.POST('/workspaces', { body: { name: name.value } }))
    creating.value = false
    name.value = ''
    router.push(`/w/${w.id}`)
  } catch (e) {
    session.error(e)
  }
}

onMounted(load)
</script>

<template>
  <div class="mx-auto max-w-6xl p-6">
    <div class="mb-6 flex items-center justify-between">
      <h1 class="text-2xl font-bold">Your workspaces</h1>
      <button class="btn-primary" @click="creating = true">New workspace</button>
    </div>
    <p v-if="loading" class="text-stone-500">Loading…</p>
    <div v-else-if="!workspaces.length" class="card-surface p-10 text-center">
      <p class="text-lg font-semibold">Welcome to Kōwhai</p>
      <p class="mt-2 text-stone-600">Create a workspace to start making boards. You can invite others once it exists.</p>
      <button class="btn-accent mt-4" @click="creating = true">Create a workspace</button>
    </div>
    <section v-for="w in workspaces" :key="w.id" class="mb-8">
      <div class="mb-3 flex items-center gap-3">
        <RouterLink :to="`/w/${w.id}`" class="text-lg font-semibold hover:underline">{{ w.name }}</RouterLink>
        <span class="rounded bg-stone-200 px-1.5 py-0.5 text-xs text-stone-600">{{ w.role }}</span>
      </div>
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
        <RouterLink
          v-for="b in boards[w.id] ?? []"
          :key="b.id"
          :to="`/b/${b.id}`"
          class="flex h-24 flex-col justify-between rounded-lg p-3 text-white shadow-sm transition-transform hover:-translate-y-0.5"
          :style="{ background: boardBackground(b.background) }"
        >
          <span class="font-semibold drop-shadow">{{ b.name }}</span>
          <span v-if="b.visibility === 'private'" class="text-xs opacity-80">Private</span>
        </RouterLink>
        <RouterLink
          :to="`/w/${w.id}?new=1`"
          class="flex h-24 items-center justify-center rounded-lg border-2 border-dashed border-stone-300 text-sm text-stone-500 hover:border-stone-400 hover:bg-white"
          >New board</RouterLink
        >
      </div>
    </section>
    <Modal v-model:open="creating" title="New workspace">
      <form class="space-y-4" @submit.prevent="create">
        <label class="block">
          <span class="label-text">Name</span>
          <input v-model="name" class="input" required maxlength="100" autofocus placeholder="e.g. Product team" />
        </label>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn-ghost" @click="creating = false">Cancel</button>
          <button class="btn-primary" :disabled="!name.trim()">Create</button>
        </div>
      </form>
    </Modal>
  </div>
</template>
