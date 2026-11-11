<script setup lang="ts">
import { computed, nextTick, ref, watchEffect } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuSeparator, DropdownMenuTrigger } from 'reka-ui'
import { api, call, type Card, type List } from '../api/client'
import { useBoard } from '../stores/board'
import { useSession } from '../stores/session'
import CardTile from './CardTile.vue'

const props = defineProps<{ list: List; filter: (c: Card) => boolean }>()
const emit = defineEmits<{ open: [string] }>()
const board = useBoard()
const session = useSession()

// A local copy the drag library can reorder. The store stays the source
// of truth and this follows it.
const cards = ref<Card[]>([])
watchEffect(() => {
  cards.value = board.cardsIn(props.list.id).filter(props.filter)
})
const filtered = computed(() => board.cardsIn(props.list.id).length !== cards.value.length)

const adding = ref(false)
const title = ref('')
const input = ref<HTMLTextAreaElement>()
const renaming = ref(false)
const name = ref('')
const templates = ref<Card[]>([])

async function startAdd() {
  adding.value = true
  await nextTick()
  input.value?.focus()
}

async function add() {
  const t = title.value.trim()
  if (!t) return
  title.value = ''
  await board.addCard(props.list.id, t)
  await nextTick()
  input.value?.focus()
}

function onEnd(evt: { item: HTMLElement; to: HTMLElement; from: HTMLElement; newIndex?: number; oldIndex?: number }) {
  const cardId = evt.item.dataset.cardId
  const toList = evt.to.dataset.listId
  if (!cardId || !toList || evt.newIndex === undefined) return
  if (evt.from === evt.to && evt.newIndex === evt.oldIndex) return
  void board.moveCard(cardId, toList, evt.newIndex)
}

function startRename() {
  if (!board.canEdit) return
  name.value = props.list.name
  renaming.value = true
}

async function rename() {
  renaming.value = false
  if (name.value.trim() && name.value !== props.list.name) await board.updateList(props.list.id, { name: name.value.trim() })
}

async function loadTemplates() {
  templates.value = await call(api.GET('/boards/{id}/card-templates', { params: { path: { id: props.list.board_id } } })).catch(() => [])
}

async function fromTemplate(t: Card) {
  const last = board.cardsIn(props.list.id).at(-1)
  try {
    await call(api.POST('/cards/{id}/copy', { params: { path: { id: t.id } }, body: { list_id: props.list.id, after_id: last?.id, as_template: false } }))
  } catch (e) {
    session.error(e)
  }
}
</script>

<template>
  <section
    class="flex max-h-full w-72 shrink-0 flex-col rounded-xl bg-stone-100/95 shadow-sm"
    :class="{ 'opacity-70': list.archived }"
    :data-list-id="list.id"
    data-testid="list"
    :aria-label="`List ${list.name}`"
  >
    <header class="list-handle flex cursor-grab items-center gap-1 px-3 pb-1 pt-2.5">
      <input
        v-if="renaming"
        v-model="name"
        class="input py-1 font-semibold"
        autofocus
        @keydown.enter="rename"
        @keydown.esc="renaming = false"
        @blur="rename"
      />
      <h2 v-else class="flex-1 truncate text-sm font-semibold text-stone-800" @click="startRename">
        {{ list.name }}
        <span class="ml-1 font-normal text-stone-400">{{ cards.length }}<template v-if="filtered">*</template></span>
      </h2>
      <DropdownMenuRoot v-if="board.canEdit" @update:open="(o: boolean) => o && loadTemplates()">
        <DropdownMenuTrigger class="rounded px-1.5 text-stone-500 hover:bg-stone-200" :aria-label="`List menu for ${list.name}`">⋯</DropdownMenuTrigger>
        <DropdownMenuPortal>
          <DropdownMenuContent class="menu" align="start">
            <DropdownMenuItem class="menu-item" @select="startAdd">Add card</DropdownMenuItem>
            <DropdownMenuItem class="menu-item" @select="startRename">Rename list</DropdownMenuItem>
            <template v-if="templates.length">
              <DropdownMenuSeparator class="my-1 h-px bg-stone-200" />
              <div class="px-2.5 py-1 text-xs font-semibold uppercase text-stone-400">From template</div>
              <DropdownMenuItem v-for="t in templates" :key="t.id" class="menu-item" @select="fromTemplate(t)">{{ t.title }}</DropdownMenuItem>
            </template>
            <DropdownMenuSeparator class="my-1 h-px bg-stone-200" />
            <DropdownMenuItem class="menu-item" @select="board.updateList(list.id, { archived: !list.archived })">
              {{ list.archived ? 'Restore list' : 'Archive list' }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenuPortal>
      </DropdownMenuRoot>
    </header>
    <VueDraggable
      v-model="cards"
      group="cards"
      :animation="150"
      :disabled="!board.canEdit"
      :data-list-id="list.id"
      class="flex min-h-2 flex-1 flex-col gap-2 overflow-y-auto px-2 pb-1"
      @end="onEnd"
    >
      <CardTile v-for="c in cards" :key="c.id" :card="c" @open="emit('open', c.id)" />
    </VueDraggable>
    <footer v-if="board.canEdit && !list.archived" class="p-2">
      <form v-if="adding" class="space-y-2" @submit.prevent="add">
        <textarea
          ref="input"
          v-model="title"
          rows="2"
          class="input resize-none"
          placeholder="Card title"
          aria-label="New card title"
          @keydown.enter.exact.prevent="add"
          @keydown.esc="adding = false"
        />
        <div class="flex gap-2">
          <button class="btn-primary">Add card</button>
          <button type="button" class="btn-ghost" @click="adding = false">Cancel</button>
        </div>
      </form>
      <button v-else class="w-full rounded-md px-2 py-1.5 text-left text-sm text-stone-600 hover:bg-stone-200" @click="startAdd">+ Add a card</button>
    </footer>
  </section>
</template>
