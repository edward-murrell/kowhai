<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, call, type Recurrence } from '../api/client'
import { longDate } from '../lib/dates'
import { useBoard } from '../stores/board'
import { useSession } from '../stores/session'

// Repeat settings for a template card.
const props = defineProps<{ cardId: string; boardId: string }>()
const board = useBoard()
const session = useSession()

const DAYS = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU']
const DAY_NAMES: Record<string, string> = { MO: 'Mon', TU: 'Tue', WE: 'Wed', TH: 'Thu', FR: 'Fri', SA: 'Sat', SU: 'Sun' }

const existing = ref<Recurrence | null>(null)
const preset = ref('weekly')
const days = ref<string[]>(['MO'])
const monthDay = ref(1)
const nth = ref('1')
const nthDay = ref('MO')
const raw = ref('FREQ=WEEKLY;BYDAY=MO')
const start = ref(defaultStart())
const tz = ref(Intl.DateTimeFormat().resolvedOptions().timeZone)
const listId = ref('')
const dueOffset = ref<number | ''>('')
const preview = ref<string[]>([])
const previewError = ref('')

function defaultStart() {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  d.setHours(9, 0, 0, 0)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T09:00`
}

const rule = computed(() => {
  switch (preset.value) {
    case 'daily':
      return 'FREQ=DAILY'
    case 'weekdays':
      return 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR'
    case 'weekly':
      return `FREQ=WEEKLY;BYDAY=${(days.value.length ? days.value : ['MO']).join(',')}`
    case 'monthly':
      return `FREQ=MONTHLY;BYMONTHDAY=${monthDay.value}`
    case 'nth':
      return `FREQ=MONTHLY;BYDAY=${nth.value === '-1' ? '-1' : '+' + nth.value}${nthDay.value}`
    default:
      return raw.value
  }
})

const timezones = (Intl as any).supportedValuesOf?.('timeZone') ?? [tz.value]

async function load() {
  const all = await call(api.GET('/boards/{id}/recurrences', { params: { path: { id: props.boardId } } }))
  existing.value = all.find((r) => r.template_card_id === props.cardId) ?? null
  if (existing.value) {
    preset.value = 'raw'
    raw.value = existing.value.rrule
    start.value = existing.value.dtstart.slice(0, 16)
    tz.value = existing.value.timezone
    listId.value = existing.value.target_list_id
    dueOffset.value = existing.value.due_offset_minutes ?? ''
  } else {
    listId.value = board.sortedLists[0]?.id ?? ''
  }
}
onMounted(() => load().catch(session.error))

let timer: ReturnType<typeof setTimeout> | undefined
watch(
  [rule, start, tz],
  () => {
    clearTimeout(timer)
    timer = setTimeout(async () => {
      try {
        const p = await call(api.POST('/recurrences/preview', { body: { rrule: rule.value, dtstart: start.value, timezone: tz.value, count: 3 } }))
        preview.value = p.times.map((t) => longDate(t))
        previewError.value = ''
      } catch (e) {
        preview.value = []
        previewError.value = e instanceof Error ? e.message : String(e)
      }
    }, 250)
  },
  { immediate: true },
)

async function save() {
  const body = {
    rrule: rule.value,
    dtstart: start.value.length === 16 ? start.value + ':00' : start.value,
    timezone: tz.value,
    target_list_id: listId.value,
    due_offset_minutes: dueOffset.value === '' ? null : Number(dueOffset.value),
  }
  try {
    if (existing.value) {
      existing.value = await call(api.PATCH('/recurrences/{id}', { params: { path: { id: existing.value.id } }, body: { ...body, template_card_id: props.cardId } }))
    } else {
      existing.value = await call(
        api.POST('/boards/{id}/recurrences', {
          params: { path: { id: props.boardId } },
          body: { ...body, template_card_id: props.cardId, due_offset_minutes: body.due_offset_minutes ?? undefined },
        }),
      )
    }
    session.toast('Repeat saved.')
  } catch (e) {
    session.error(e)
  }
}

async function setPaused(paused: boolean) {
  if (!existing.value) return
  existing.value = await call(api.PATCH('/recurrences/{id}', { params: { path: { id: existing.value.id } }, body: { paused } })).catch((e) => {
    session.error(e)
    return existing.value
  })
}

async function runNow() {
  if (!existing.value) return
  await call(api.POST('/recurrences/{id}/run', { params: { path: { id: existing.value.id } } })).catch(session.error)
  session.toast('Card created.')
}

async function remove() {
  if (!existing.value || !confirm('Stop repeating this card?')) return
  await call(api.DELETE('/recurrences/{id}', { params: { path: { id: existing.value.id } } })).catch(session.error)
  existing.value = null
}
</script>

<template>
  <div class="space-y-3 text-sm">
    <p v-if="existing" class="rounded bg-stone-100 px-3 py-2">
      <template v-if="existing.paused"><strong>Paused.</strong></template>
      <template v-else-if="existing.next_run_at">Next card: <strong>{{ longDate(existing.next_run_at) }}</strong></template>
      <template v-if="existing.last_run_at"> · Last: {{ longDate(existing.last_run_at) }}</template>
    </p>
    <div class="grid grid-cols-2 gap-3">
      <label class="col-span-2">
        <span class="label-text">Repeats</span>
        <select v-model="preset" class="input">
          <option value="daily">Every day</option>
          <option value="weekdays">Every weekday</option>
          <option value="weekly">Weekly on chosen days</option>
          <option value="monthly">Monthly on a date</option>
          <option value="nth">Monthly on the nth weekday</option>
          <option value="raw">Custom rule (RRULE)</option>
        </select>
      </label>
      <div v-if="preset === 'weekly'" class="col-span-2 flex gap-1">
        <button
          v-for="d in DAYS"
          :key="d"
          type="button"
          class="rounded px-2 py-1 text-xs font-medium"
          :class="days.includes(d) ? 'bg-bush-800 text-white' : 'bg-stone-200'"
          @click="days = days.includes(d) ? days.filter((x) => x !== d) : DAYS.filter((x) => days.includes(x) || x === d)"
        >
          {{ DAY_NAMES[d] }}
        </button>
      </div>
      <label v-if="preset === 'monthly'"><span class="label-text">Day of month</span><input v-model.number="monthDay" type="number" min="1" max="31" class="input" /></label>
      <template v-if="preset === 'nth'">
        <select v-model="nth" class="input" aria-label="Which week">
          <option value="1">First</option><option value="2">Second</option><option value="3">Third</option><option value="4">Fourth</option><option value="-1">Last</option>
        </select>
        <select v-model="nthDay" class="input" aria-label="Weekday">
          <option v-for="d in DAYS" :key="d" :value="d">{{ DAY_NAMES[d] }}</option>
        </select>
      </template>
      <label v-if="preset === 'raw'" class="col-span-2"
        ><span class="label-text">RRULE</span><input v-model="raw" class="input font-mono" placeholder="FREQ=WEEKLY;BYDAY=MO,WE"
      /></label>
      <label><span class="label-text">Starting</span><input v-model="start" type="datetime-local" class="input" /></label>
      <label>
        <span class="label-text">Time zone</span>
        <select v-model="tz" class="input">
          <option v-for="z in timezones" :key="z" :value="z">{{ z }}</option>
        </select>
      </label>
      <label>
        <span class="label-text">Create in list</span>
        <select v-model="listId" class="input">
          <option v-for="l in board.sortedLists" :key="l.id" :value="l.id">{{ l.name }}</option>
        </select>
      </label>
      <label><span class="label-text">Due after (minutes)</span><input v-model="dueOffset" type="number" min="0" class="input" placeholder="No due date" /></label>
    </div>
    <p class="font-mono text-xs text-stone-500">{{ rule }}</p>
    <p v-if="previewError" class="text-red-700">{{ previewError }}</p>
    <ul v-else-if="preview.length" class="text-stone-600">
      <li v-for="p in preview" :key="p">→ {{ p }}</li>
    </ul>
    <div class="flex flex-wrap gap-2">
      <button class="btn-primary" :disabled="!!previewError || !listId" @click="save">{{ existing ? 'Save repeat' : 'Start repeating' }}</button>
      <template v-if="existing">
        <button class="btn-ghost" @click="setPaused(!existing.paused)">{{ existing.paused ? 'Resume' : 'Pause' }}</button>
        <button class="btn-ghost" @click="runNow">Create one now</button>
        <button class="btn-ghost text-red-700" @click="remove">Stop repeating</button>
      </template>
    </div>
  </div>
</template>
