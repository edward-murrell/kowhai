<script setup lang="ts">
import type { Label } from '../api/client'
import { labelStyle } from '../lib/colours'

withDefaults(defineProps<{ label: Label; compact?: boolean }>(), { compact: false })
</script>

<template>
  <span
    v-if="compact"
    class="inline-block h-2 w-10 rounded-full"
    :style="labelStyle(label.colour)"
    :title="label.name || label.colour"
  />
  <span v-else class="inline-flex h-6 items-center rounded px-2 text-xs font-semibold" :style="labelStyle(label.colour)">{{
    label.name || ' '
  }}</span>
</template>
