<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, call, type S } from '../api/client'
import { useSession } from '../stores/session'

const route = useRoute()
const router = useRouter()
const session = useSession()
const q = ref('')
const results = ref<S['SearchResults'] | null>(null)
const loading = ref(false)

async function run() {
  if (!q.value.trim()) {
    results.value = null
    return
  }
  loading.value = true
  try {
    results.value = await call(api.GET('/search', { params: { query: { q: q.value, limit: 100 } } }))
  } catch (e) {
    session.error(e)
  } finally {
    loading.value = false
  }
}

watch(
  () => route.query.q,
  (v) => {
    q.value = typeof v === 'string' ? v : ''
    void run()
  },
  { immediate: true },
)

// Snippets mark matches with [[ and ]]. Split them for highlighting
// without using v-html.
function parts(s: string): { text: string; hit: boolean }[] {
  return s.split(/(\[\[.*?\]\])/).filter(Boolean).map((p) => (p.startsWith('[[') ? { text: p.slice(2, -2), hit: true } : { text: p, hit: false }))
}

const where: Record<string, string> = { card: 'Card', comment: 'Comment', checklist_item: 'Checklist' }
</script>

<template>
  <div class="mx-auto max-w-3xl p-6">
    <form class="flex gap-2" @submit.prevent="router.replace({ query: { q } })">
      <input v-model="q" type="search" class="input" placeholder="Search cards" aria-label="Search" autofocus />
      <button class="btn-primary">Search</button>
    </form>
    <p class="mt-2 text-xs text-stone-500">Filters: <code>label:bug</code> <code>member:alice</code> <code>due:overdue</code> <code>is:archived</code></p>
    <p v-if="loading" class="mt-6 text-stone-500">Searching…</p>
    <p v-else-if="results && !results.groups.length" class="mt-6 text-stone-500">No matches.</p>
    <section v-for="g in results?.groups ?? []" :key="g.board_id" class="mt-6">
      <h2 class="mb-2 font-semibold">
        <RouterLink :to="`/b/${g.board_id}`" class="hover:underline">{{ g.board_name }}</RouterLink>
      </h2>
      <ul class="card-surface divide-y divide-stone-100">
        <li v-for="(h, i) in g.hits" :key="i">
          <RouterLink :to="{ name: 'board', params: { id: g.board_id }, query: { card: h.card_id } }" class="block px-4 py-3 hover:bg-stone-50">
            <div class="font-medium">
              {{ h.card_title }}
              <span v-if="h.archived" class="ml-1 rounded bg-stone-200 px-1 text-xs">archived</span>
            </div>
            <div class="mt-0.5 text-sm text-stone-600">
              <span class="mr-1 text-xs uppercase text-stone-400">{{ where[h.entity_type] }}</span>
              <template v-for="(p, j) in parts(h.snippet)" :key="j"
                ><mark v-if="p.hit" class="rounded bg-kowhai-100 px-0.5">{{ p.text }}</mark><template v-else>{{ p.text }}</template></template
              >
            </div>
          </RouterLink>
        </li>
      </ul>
    </section>
  </div>
</template>
