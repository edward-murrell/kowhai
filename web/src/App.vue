<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppHeader from './components/AppHeader.vue'
import Toasts from './components/Toasts.vue'
import { useSession } from './stores/session'

const session = useSession()
const ready = ref(false)
const failed = ref('')

onMounted(async () => {
  try {
    await session.load()
    ready.value = true
  } catch (e) {
    // A 401 redirects to sign-in. Anything else is shown here.
    failed.value = e instanceof Error ? e.message : String(e)
  }
})
</script>

<template>
  <div v-if="ready" class="flex h-full flex-col">
    <AppHeader />
    <main class="min-h-0 flex-1">
      <RouterView />
    </main>
    <Toasts />
  </div>
  <div v-else-if="failed" class="flex h-full items-center justify-center p-8">
    <div class="card-surface max-w-md p-6 text-center">
      <p class="font-semibold">Kōwhai could not load.</p>
      <p class="mt-2 text-sm text-stone-600">{{ failed }}</p>
      <a href="/auth/login" class="btn-primary mt-4">Sign in</a>
    </div>
  </div>
  <div v-else class="flex h-full items-center justify-center text-stone-400">Loading…</div>
</template>
