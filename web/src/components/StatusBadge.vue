<script setup lang="ts">
import { computed } from 'vue'
import type { CardLink } from '../api/client'
import { ago } from '../lib/dates'

const props = defineProps<{ link: CardLink; count?: number; full?: boolean }>()
const style = computed(
  () =>
    ({
      waiting: 'bg-amber-100 text-amber-900 ring-amber-300',
      working: 'bg-emerald-100 text-emerald-900 ring-emerald-300',
      idle: 'bg-sky-100 text-sky-900 ring-sky-300',
      stale: 'bg-stone-200 text-stone-700 ring-stone-300',
      ended: 'bg-stone-100 text-stone-500 ring-stone-200',
    })[props.link.status ?? 'ended'],
)
const label = computed(
  () =>
    ({ waiting: 'Waiting on you', working: 'Working', idle: 'Idle', stale: 'Stale', ended: 'Ended' })[props.link.status ?? 'ended'],
)
</script>

<template>
  <span
    class="inline-flex max-w-full items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ring-1"
    :class="style"
    :title="link.status_detail || label"
  >
    <span
      class="h-1.5 w-1.5 shrink-0 rounded-full bg-current"
      :class="{ 'animate-pulse': link.status === 'working' || link.status === 'waiting' }"
    />
    <span class="font-semibold">Claude</span>
    <span>{{ label }}</span>
    <span v-if="count && count > 1">×{{ count }}</span>
    <span v-if="link.branch" class="truncate font-mono opacity-80">{{ link.branch }}</span>
    <span v-if="full && link.last_seen_at" class="opacity-70">· {{ ago(link.last_seen_at) }}</span>
  </span>
</template>
