<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useBoard } from '../stores/board'
import Markdown from './Markdown.vue'

// A textarea with a preview tab and @mention suggestions from the board.
const model = defineModel<string>({ default: '' })
withDefaults(defineProps<{ rows?: number; placeholder?: string; label?: string }>(), { rows: 6, placeholder: '', label: 'Text' })
const emit = defineEmits<{ submit: [] }>()
const board = useBoard()
const preview = ref(false)
const ta = ref<HTMLTextAreaElement>()
const query = ref<string | null>(null)

function handle(name: string) {
  return name.replace(/\s+/g, '')
}

const suggestions = computed(() => {
  if (query.value === null) return []
  const q = query.value.toLowerCase()
  return board.users.filter((u) => handle(u.display_name).toLowerCase().startsWith(q)).slice(0, 6)
})

function onInput() {
  const el = ta.value
  if (!el) return
  const before = el.value.slice(0, el.selectionStart)
  const m = before.match(/(?:^|\s)@([\w.-]*)$/)
  query.value = m ? m[1] : null
}

async function pick(name: string) {
  const el = ta.value!
  const pos = el.selectionStart
  const before = el.value.slice(0, pos).replace(/@([\w.-]*)$/, '@' + handle(name) + ' ')
  model.value = before + el.value.slice(pos)
  query.value = null
  await nextTick()
  el.focus()
  el.selectionStart = el.selectionEnd = before.length
}

function focus() {
  ta.value?.focus()
}
defineExpose({ focus })
</script>

<template>
  <div class="rounded-md border border-stone-300 bg-white focus-within:border-bush-700">
    <div class="flex gap-1 border-b border-stone-200 px-2 pt-1 text-xs">
      <button type="button" class="rounded-t px-2 py-1" :class="!preview ? 'bg-stone-100 font-semibold' : 'text-stone-500'" @click="preview = false">Write</button>
      <button type="button" class="rounded-t px-2 py-1" :class="preview ? 'bg-stone-100 font-semibold' : 'text-stone-500'" @click="preview = true">Preview</button>
      <span class="ml-auto py-1 text-stone-400">Markdown, @mentions</span>
    </div>
    <div class="relative">
      <textarea
        v-show="!preview"
        ref="ta"
        v-model="model"
        :rows="rows"
        :placeholder="placeholder"
        :aria-label="label"
        class="block w-full resize-y rounded-b-md px-3 py-2 text-sm outline-none"
        @input="onInput"
        @keydown.ctrl.enter="emit('submit')"
        @keydown.meta.enter="emit('submit')"
        @keydown.esc="query = null"
      />
      <ul v-if="suggestions.length && !preview" class="absolute left-2 top-full z-20 w-60 rounded-md bg-white shadow-lg ring-1 ring-stone-900/10">
        <li v-for="u in suggestions" :key="u.id">
          <button type="button" class="w-full px-3 py-1.5 text-left text-sm hover:bg-stone-100" @mousedown.prevent="pick(u.display_name)">
            {{ u.display_name }} <span class="text-stone-400">@{{ handle(u.display_name) }}</span>
          </button>
        </li>
      </ul>
      <div v-if="preview" class="min-h-24 px-3 py-2"><Markdown :src="model || '_Nothing to preview._'" /></div>
    </div>
  </div>
</template>
