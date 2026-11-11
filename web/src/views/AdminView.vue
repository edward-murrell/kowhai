<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { api, call, type User } from '../api/client'
import { shortDate } from '../lib/dates'
import { useSession } from '../stores/session'
import Avatar from '../components/Avatar.vue'

const session = useSession()
const users = ref<User[]>([])
const cursor = ref<string | undefined>()
const q = ref('')

async function load(more = false) {
  try {
    const page = await call(api.GET('/admin/users', { params: { query: { q: q.value || undefined, cursor: more ? cursor.value : undefined } } }))
    users.value = more ? [...users.value, ...page.items] : page.items
    cursor.value = page.next_cursor
  } catch (e) {
    session.error(e)
  }
}
onMounted(() => load())
let timer: ReturnType<typeof setTimeout> | undefined
watch(q, () => {
  clearTimeout(timer)
  timer = setTimeout(() => load(), 250)
})

async function toggle(u: User) {
  if (!confirm(u.disabled ? `Enable ${u.display_name}?` : `Disable ${u.display_name}? They are signed out and their tokens stop working.`)) return
  try {
    Object.assign(u, await call(api.PATCH('/admin/users/{userId}', { params: { path: { userId: u.id } }, body: { disabled: !u.disabled } })))
  } catch (e) {
    session.error(e)
  }
}
</script>

<template>
  <div class="mx-auto max-w-4xl p-6">
    <h1 class="mb-4 text-2xl font-bold">Users</h1>
    <p v-if="!session.user?.is_admin" class="text-stone-600">You need to be a site admin to see this page.</p>
    <template v-else>
      <input v-model="q" type="search" class="input mb-4 max-w-sm" placeholder="Filter by name or email" aria-label="Filter users" />
      <table class="card-surface w-full text-sm">
        <thead class="text-left text-xs uppercase text-stone-500">
          <tr><th class="px-4 py-2">User</th><th class="px-4 py-2">Joined</th><th class="px-4 py-2">Status</th><th /></tr>
        </thead>
        <tbody class="divide-y divide-stone-100">
          <tr v-for="u in users" :key="u.id">
            <td class="flex items-center gap-3 px-4 py-2">
              <Avatar :name="u.display_name" :url="u.avatar_url" />
              <div>
                <div class="font-medium">{{ u.display_name }} <span v-if="u.is_admin" class="ml-1 rounded bg-kowhai-100 px-1 text-xs">admin</span></div>
                <div class="text-stone-500">{{ u.email }}</div>
              </div>
            </td>
            <td class="px-4 py-2">{{ shortDate(u.created_at) }}</td>
            <td class="px-4 py-2">{{ u.disabled ? 'Disabled' : 'Active' }}</td>
            <td class="px-4 py-2 text-right">
              <button v-if="u.id !== session.user?.id" class="btn-ghost" :class="{ 'text-red-700': !u.disabled }" @click="toggle(u)">
                {{ u.disabled ? 'Enable' : 'Disable' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <button v-if="cursor" class="btn-ghost mt-3" @click="load(true)">Load more</button>
    </template>
  </div>
</template>
