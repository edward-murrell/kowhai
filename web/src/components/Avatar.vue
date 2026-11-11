<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{ name?: string; url?: string; size?: number }>(), { name: '?', url: '', size: 28 })
const initials = computed(() =>
  (props.name || '?')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join(''),
)
// A stable colour per name.
const hue = computed(() => [...(props.name || '')].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 360, 7))
</script>

<template>
  <img v-if="url" :src="url" :alt="name" :title="name" class="shrink-0 rounded-full object-cover" :style="{ width: size + 'px', height: size + 'px' }" />
  <span
    v-else
    :title="name"
    class="inline-flex shrink-0 select-none items-center justify-center rounded-full font-semibold text-white"
    :style="{ width: size + 'px', height: size + 'px', fontSize: size * 0.4 + 'px', backgroundColor: `hsl(${hue} 45% 42%)` }"
    >{{ initials }}</span
  >
</template>
