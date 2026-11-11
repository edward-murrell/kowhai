<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, call, type Board, type S } from '../api/client'
import { BACKGROUNDS, boardBackground } from '../lib/colours'
import { useSession } from '../stores/session'
import Modal from './Modal.vue'

const props = defineProps<{ workspaceId: string }>()
const open = defineModel<boolean>('open', { default: false })
const session = useSession()
const router = useRouter()

const name = ref('')
const visibility = ref<'workspace' | 'private'>('workspace')
const background = ref('bush')
const template = ref('') // '' for blank, a board ID, or builtin:<key>
const includeCards = ref(false)
const templates = ref<Board[]>([])
const builtins = ref<S['BuiltinTemplate'][]>([])
const busy = ref(false)

watch(open, async (v) => {
  if (!v) return
  const id = props.workspaceId
  try {
    ;[templates.value, builtins.value] = await Promise.all([
      call(api.GET('/workspaces/{id}/templates', { params: { path: { id } } })),
      call(api.GET('/workspaces/{id}/templates/builtin', { params: { path: { id } } })),
    ])
  } catch (e) {
    session.error(e)
  }
})

async function create() {
  busy.value = true
  try {
    let srcId = template.value
    if (srcId.startsWith('builtin:')) {
      const tb = await call(
        api.POST('/workspaces/{id}/templates/builtin', { params: { path: { id: props.workspaceId } }, body: { key: srcId.slice(8) } }),
      )
      srcId = tb.id
    }
    let b: Board
    if (srcId) {
      b = await call(
        api.POST('/boards/{id}/copy', {
          params: { path: { id: srcId } },
          body: { name: name.value, workspace_id: props.workspaceId, include_cards: includeCards.value, visibility: visibility.value },
        }),
      )
      if (b.background !== background.value) {
        b = await call(api.PATCH('/boards/{id}', { params: { path: { id: b.id } }, body: { background: background.value } }))
      }
    } else {
      b = await call(
        api.POST('/workspaces/{id}/boards', {
          params: { path: { id: props.workspaceId } },
          body: { name: name.value, visibility: visibility.value, background: background.value },
        }),
      )
    }
    open.value = false
    router.push(`/b/${b.id}`)
  } catch (e) {
    session.error(e)
  } finally {
    busy.value = false
  }
}

const builtinsNotAdded = () => builtins.value.filter((b) => !b.board_id)
</script>

<template>
  <Modal v-model:open="open" title="New board">
    <form class="space-y-4" @submit.prevent="create">
      <div class="flex h-20 items-end rounded-lg p-3 font-semibold text-white" :style="{ background: boardBackground(background) }">
        {{ name || 'Board name' }}
      </div>
      <label class="block">
        <span class="label-text">Name</span>
        <input v-model="name" class="input" required maxlength="200" autofocus />
      </label>
      <div>
        <span class="label-text">Background</span>
        <div class="flex gap-2">
          <button
            v-for="(_, key) in BACKGROUNDS"
            :key="key"
            type="button"
            class="h-8 w-12 rounded ring-offset-2"
            :class="{ 'ring-2 ring-bush-700': background === key }"
            :style="{ background: boardBackground(key) }"
            :aria-label="`Background ${key}`"
            @click="background = key"
          />
        </div>
      </div>
      <label class="block">
        <span class="label-text">Template</span>
        <select v-model="template" class="input" data-testid="template-select">
          <option value="">Blank board</option>
          <optgroup v-if="templates.length" label="This workspace">
            <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.name }}</option>
          </optgroup>
          <optgroup v-if="builtinsNotAdded().length" label="Built in">
            <option v-for="t in builtinsNotAdded()" :key="t.key" :value="`builtin:${t.key}`">{{ t.name }} — {{ t.description }}</option>
          </optgroup>
        </select>
      </label>
      <label v-if="template" class="flex items-center gap-2 text-sm">
        <input v-model="includeCards" type="checkbox" /> Copy cards and checklists too
      </label>
      <label class="block">
        <span class="label-text">Who can see it</span>
        <select v-model="visibility" class="input">
          <option value="workspace">Everyone in the workspace</option>
          <option value="private">Only board members and workspace admins</option>
        </select>
      </label>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn-ghost" @click="open = false">Cancel</button>
        <button class="btn-primary" :disabled="busy || !name.trim()">Create board</button>
      </div>
    </form>
  </Modal>
</template>
