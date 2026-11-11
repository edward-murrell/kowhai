<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, call } from '../api/client'

// /c/:id opens a card on its board.
const props = defineProps<{ id: string }>()
const router = useRouter()
const error = ref('')

onMounted(async () => {
  try {
    const c = await call(api.GET('/cards/{id}', { params: { path: { id: props.id } } }))
    router.replace({ name: 'board', params: { id: c.board_id }, query: { card: c.id } })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
})
</script>

<template>
  <div class="p-8 text-center text-stone-600">{{ error || 'Opening card…' }}</div>
</template>
