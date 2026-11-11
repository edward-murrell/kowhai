<script setup lang="ts">
import { useSession } from '../stores/session'
const session = useSession()
</script>

<template>
  <div class="pointer-events-none fixed bottom-4 right-4 z-[60] flex flex-col gap-2" aria-live="polite">
    <div
      v-for="t in session.toasts"
      :key="t.id"
      class="pointer-events-auto flex max-w-sm items-start gap-3 rounded-lg px-4 py-3 text-sm shadow-lg"
      :class="t.kind === 'error' ? 'bg-red-600 text-white' : 'bg-bush-800 text-white'"
      role="status"
    >
      <span class="flex-1">{{ t.text }}</span>
      <button class="opacity-70 hover:opacity-100" aria-label="Dismiss" @click="session.dismiss(t.id)">✕</button>
    </div>
  </div>
</template>
