<script setup lang="ts">
import { DialogClose, DialogContent, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'

const open = defineModel<boolean>('open', { default: false })
withDefaults(defineProps<{ title: string; wide?: boolean }>(), { wide: false })
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-stone-900/50" />
      <DialogContent
        class="fixed left-1/2 top-[5vh] z-50 max-h-[90vh] w-[calc(100vw-2rem)] -translate-x-1/2 overflow-y-auto rounded-xl bg-stone-50 shadow-2xl outline-none"
        :class="wide ? 'max-w-4xl' : 'max-w-lg'"
        :aria-describedby="undefined"
      >
        <div class="sticky top-0 z-10 flex items-center justify-between border-b border-stone-200 bg-stone-50 px-5 py-3">
          <DialogTitle class="text-base font-semibold"><slot name="title">{{ title }}</slot></DialogTitle>
          <DialogClose class="btn-ghost px-2" aria-label="Close">✕</DialogClose>
        </div>
        <div class="p-5"><slot /></div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
