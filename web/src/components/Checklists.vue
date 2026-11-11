<script setup lang="ts">
import { ref } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { api, call, type Checklist, type ChecklistItem } from '../api/client'
import { useSession } from '../stores/session'

const props = defineProps<{ checklists: Checklist[]; editable: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const session = useSession()
const newItem = ref<Record<string, string>>({})
const hideDone = ref<Record<string, boolean>>({})
const editing = ref('')
const editText = ref('')

async function run<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>) {
  try {
    await call(p)
  } catch (e) {
    session.error(e)
  }
  emit('changed')
}

function toggle(i: ChecklistItem) {
  i.done = !i.done
  void run(api.PATCH('/checklist-items/{id}', { params: { path: { id: i.id } }, body: { done: i.done } }))
}

function add(cl: Checklist) {
  const text = newItem.value[cl.id]?.trim()
  if (!text) return
  newItem.value[cl.id] = ''
  void run(api.POST('/checklists/{id}/items', { params: { path: { id: cl.id } }, body: { text } }))
}

function startEdit(i: ChecklistItem) {
  if (!props.editable) return
  editing.value = i.id
  editText.value = i.text
}

function saveEdit(i: ChecklistItem) {
  editing.value = ''
  if (editText.value.trim() && editText.value !== i.text) {
    void run(api.PATCH('/checklist-items/{id}', { params: { path: { id: i.id } }, body: { text: editText.value.trim() } }))
  }
}

function removeItem(i: ChecklistItem) {
  void run(api.DELETE('/checklist-items/{id}', { params: { path: { id: i.id } } }))
}

function removeList(cl: Checklist) {
  if (confirm(`Delete the checklist ${cl.title}?`)) void run(api.DELETE('/checklists/{id}', { params: { path: { id: cl.id } } }))
}

function onDrop(cl: Checklist, evt: { item: HTMLElement; to: HTMLElement; newIndex?: number }) {
  const id = evt.item.dataset.itemId
  const target = evt.to.dataset.checklistId
  if (!id || !target || evt.newIndex === undefined) return
  const items = (props.checklists.find((c) => c.id === target)?.items ?? []).filter((x) => x.id !== id)
  const after = items[evt.newIndex - 1]
  const before = items[evt.newIndex]
  void run(
    api.PATCH('/checklist-items/{id}', {
      params: { path: { id } },
      body: { checklist_id: target, after_id: after?.id, before_id: after ? undefined : before?.id },
    }),
  )
  void cl
}

const pct = (cl: Checklist) => (cl.items.length ? Math.round((cl.items.filter((i) => i.done).length / cl.items.length) * 100) : 0)
</script>

<template>
  <section v-for="cl in checklists" :key="cl.id" class="space-y-2" :aria-label="`Checklist ${cl.title}`">
    <div class="flex items-center gap-2">
      <h3 class="flex-1 font-semibold">☑ {{ cl.title }}</h3>
      <button class="btn-ghost text-xs" @click="hideDone[cl.id] = !hideDone[cl.id]">{{ hideDone[cl.id] ? 'Show done' : 'Hide done' }}</button>
      <button v-if="editable" class="btn-ghost text-xs" @click="removeList(cl)">Delete</button>
    </div>
    <div class="flex items-center gap-2 text-xs text-stone-500">
      <span class="w-8">{{ pct(cl) }}%</span>
      <div class="h-1.5 flex-1 rounded bg-stone-200">
        <div class="h-1.5 rounded transition-all" :class="pct(cl) === 100 ? 'bg-emerald-500' : 'bg-bush-700'" :style="{ width: pct(cl) + '%' }" />
      </div>
    </div>
    <VueDraggable
      :model-value="cl.items"
      group="checklist-items"
      :disabled="!editable"
      :animation="150"
      :data-checklist-id="cl.id"
      class="space-y-0.5"
      @end="(e: any) => onDrop(cl, e)"
    >
      <div
        v-for="i in cl.items"
        v-show="!(hideDone[cl.id] && i.done)"
        :key="i.id"
        :data-item-id="i.id"
        class="group flex items-start gap-2 rounded px-1 py-1 hover:bg-stone-100"
      >
        <input type="checkbox" class="mt-1" :checked="i.done" :disabled="!editable" :aria-label="i.text" @change="toggle(i)" />
        <input
          v-if="editing === i.id"
          v-model="editText"
          class="input py-0.5"
          autofocus
          @keydown.enter="saveEdit(i)"
          @keydown.esc="editing = ''"
          @blur="saveEdit(i)"
        />
        <span v-else class="flex-1 text-sm" :class="{ 'text-stone-400 line-through': i.done }" @click="startEdit(i)">{{ i.text }}</span>
        <button v-if="editable" class="invisible text-xs text-stone-400 hover:text-red-600 group-hover:visible" :aria-label="`Delete ${i.text}`" @click="removeItem(i)">
          ✕
        </button>
      </div>
    </VueDraggable>
    <form v-if="editable" class="flex gap-2" @submit.prevent="add(cl)">
      <input v-model="newItem[cl.id]" class="input" placeholder="Add an item" :aria-label="`Add item to ${cl.title}`" />
      <button class="btn-ghost">Add</button>
    </form>
  </section>
</template>
