<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuSeparator, DropdownMenuTrigger } from 'reka-ui'
import { logout } from '../api/client'
import { useSession } from '../stores/session'
import Avatar from './Avatar.vue'
import NotificationsPanel from './NotificationsPanel.vue'

const session = useSession()
const router = useRouter()
const q = ref('')

function openDocs() {
  window.location.href = "/api/docs"
}

function search() {
  if (q.value.trim()) router.push({ name: 'search', query: { q: q.value.trim() } })
}
</script>

<template>
  <header class="flex h-12 shrink-0 items-center gap-3 bg-bush-900 px-3 text-white">
    <RouterLink to="/" class="flex items-center gap-2 font-semibold tracking-tight">
      <img src="/favicon.svg" alt="" class="h-7 w-7" />
      <span>Kōwhai</span>
    </RouterLink>
    <RouterLink to="/" class="rounded px-2 py-1 text-sm text-white/80 hover:bg-white/10 hover:text-white">Workspaces</RouterLink>
    <form class="ml-auto w-full max-w-xs" role="search" @submit.prevent="search">
      <input
        v-model="q"
        type="search"
        placeholder="Search cards… (label:, member:, due:overdue)"
        aria-label="Search cards"
        class="w-full rounded-md bg-white/10 px-3 py-1.5 text-sm text-white placeholder-white/50 outline-none focus:bg-white focus:text-stone-900 focus:placeholder-stone-400"
      />
    </form>
    <NotificationsPanel />
    <DropdownMenuRoot>
      <DropdownMenuTrigger class="rounded-full ring-white/40 hover:ring-2" aria-label="Account menu">
        <Avatar :name="session.user?.display_name" :url="session.user?.avatar_url" :size="30" />
      </DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent class="menu" align="end" :side-offset="6">
          <div class="px-2.5 py-2 text-sm">
            <div class="font-semibold">{{ session.user?.display_name }}</div>
            <div class="text-stone-500">{{ session.user?.email }}</div>
          </div>
          <DropdownMenuSeparator class="my-1 h-px bg-stone-200" />
          <DropdownMenuItem class="menu-item" @select="router.push('/account')">Account settings</DropdownMenuItem>
          <DropdownMenuItem v-if="session.user?.is_admin" class="menu-item" @select="router.push('/admin')">Admin</DropdownMenuItem>
          <DropdownMenuItem class="menu-item" @select="openDocs">API docs</DropdownMenuItem>
          <DropdownMenuSeparator class="my-1 h-px bg-stone-200" />
          <DropdownMenuItem class="menu-item" @select="logout">Sign out</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  </header>
</template>
