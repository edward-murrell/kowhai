<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, call, type User } from '../api/client'

// UserPicker searches all users by name or email.
const emit = defineEmits<{ pick: [User] }>()
withDefaults(defineProps<{ placeholder?: string }>(), { placeholder: 'Find people by name or email' })
const q = ref('')
const results = ref<User[]>([])
let timer: ReturnType<typeof setTimeout> | undefined

watch(q, (v) => {
  clearTimeout(timer)
  if (v.trim().length < 2) {
    results.value = []
    return
  }
  timer = setTimeout(async () => {
    results.value = await call(api.GET('/users', { params: { query: { q: v.trim() } } })).catch(() => [])
  }, 200)
})

function pick(u: User) {
  emit('pick', u)
  q.value = ''
  results.value = []
}
</script>

<template>
  <div class="relative">
    <input v-model="q" class="input" :placeholder="placeholder" aria-label="Find a person" />
    <ul v-if="results.length" class="absolute z-20 mt-1 w-full overflow-hidden rounded-md bg-white shadow-lg ring-1 ring-stone-900/10">
      <li v-for="u in results" :key="u.id">
        <button type="button" class="w-full px-3 py-2 text-left text-sm hover:bg-stone-100" @click="pick(u)">
          <span class="font-medium">{{ u.display_name }}</span> <span class="text-stone-500">{{ u.email }}</span>
        </button>
      </li>
    </ul>
  </div>
</template>
