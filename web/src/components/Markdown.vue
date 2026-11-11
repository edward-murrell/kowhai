<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '../lib/markdown'

const props = defineProps<{ src: string }>()
const html = computed(() => renderMarkdown(props.src))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- sanitised with DOMPurify -->
  <div class="prose-kowhai break-words" v-html="html" />
</template>
