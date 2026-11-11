<script setup lang="ts">
import { computed } from 'vue'
import type { Card } from '../api/client'
import { dueState, shortDate } from '../lib/dates'
import { useBoard } from '../stores/board'
import Avatar from './Avatar.vue'
import LabelChip from './LabelChip.vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ card: Card }>()
defineEmits<{ open: [] }>()
const board = useBoard()
const labels = computed(() => props.card.label_ids.map((id) => board.labelById(id)).filter((l) => !!l))
const members = computed(() => props.card.member_ids.map((id) => board.userById(id)).filter((u) => !!u))
const session = computed(() => board.cardSessions(props.card.id))
const due = computed(() => dueState(props.card.due_at, props.card.due_done))
const dueClass = computed(
  () =>
    ({ done: 'bg-emerald-600 text-white', overdue: 'bg-red-600 text-white', soon: 'bg-amber-400 text-amber-950', later: 'bg-stone-100 text-stone-700', '': '' })[
      due.value
    ],
)
const cover = computed(() => (props.card.cover_attachment_id ? `/api/v1/attachments/${props.card.cover_attachment_id}/download` : ''))
</script>

<template>
  <div
    class="group cursor-pointer overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-stone-900/5 hover:ring-bush-700/40"
    :class="{ 'opacity-60': card.archived }"
    :data-card-id="card.id"
    data-testid="card-tile"
    tabindex="0"
    role="button"
    :aria-label="`Open card ${card.title}`"
    @click="$emit('open')"
    @keydown.enter="$emit('open')"
  >
    <img v-if="cover" :src="cover" alt="" class="max-h-40 w-full object-cover" loading="lazy" />
    <div class="space-y-1.5 p-2.5">
      <div v-if="labels.length" class="flex flex-wrap gap-1">
        <LabelChip v-for="l in labels" :key="l!.id" :label="l!" compact />
      </div>
      <div class="text-sm leading-snug text-stone-900">{{ card.title }}</div>
      <StatusBadge v-if="session.top" :link="session.top" :count="session.count" />
      <div
        v-if="card.due_at || card.description || card.counts.comments || card.counts.checklist_items || card.counts.attachments || members.length"
        class="flex flex-wrap items-center gap-2 text-xs text-stone-500"
      >
        <span v-if="card.due_at" class="rounded px-1.5 py-0.5 font-medium" :class="dueClass">{{ shortDate(card.due_at) }}</span>
        <span v-if="card.description" title="Has a description">≡</span>
        <span v-if="card.counts.comments" title="Comments">💬 {{ card.counts.comments }}</span>
        <span v-if="card.counts.attachments" title="Attachments">📎 {{ card.counts.attachments }}</span>
        <span
          v-if="card.counts.checklist_items"
          title="Checklist"
          :class="{ 'rounded bg-emerald-600 px-1 text-white': card.counts.checklist_done === card.counts.checklist_items }"
          >☑ {{ card.counts.checklist_done }}/{{ card.counts.checklist_items }}</span
        >
        <span class="ml-auto flex -space-x-1.5">
          <Avatar v-for="m in members" :key="m!.id" :name="m!.display_name" :url="m!.avatar_url" :size="22" class="ring-2 ring-white" />
        </span>
      </div>
    </div>
  </div>
</template>
