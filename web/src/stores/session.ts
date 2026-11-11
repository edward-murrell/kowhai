import { defineStore } from 'pinia'
import { api, call, type Me, type Notification } from '../api/client'

export interface Toast {
  id: number
  text: string
  kind: 'error' | 'info'
}

let toastId = 0

export const useSession = defineStore('session', {
  state: () => ({
    me: null as Me | null,
    unread: 0,
    toasts: [] as Toast[],
    latest: null as Notification | null,
    stream: null as EventSource | null,
  }),
  getters: {
    user: (s) => s.me?.user,
  },
  actions: {
    async load() {
      this.me = await call(api.GET('/me'))
      this.unread = this.me.unread_notifications
      this.listen()
    },
    listen() {
      if (this.stream) return
      const es = new EventSource('/api/v1/me/events')
      es.addEventListener('unread', (e) => {
        this.unread = JSON.parse((e as MessageEvent).data).unread
      })
      es.addEventListener('notification', (e) => {
        this.latest = JSON.parse((e as MessageEvent).data)
      })
      this.stream = es
    },
    toast(text: string, kind: Toast['kind'] = 'info') {
      const t = { id: ++toastId, text, kind }
      this.toasts.push(t)
      setTimeout(() => this.dismiss(t.id), kind === 'error' ? 8000 : 4000)
    },
    error(e: unknown) {
      this.toast(e instanceof Error ? e.message : String(e), 'error')
    },
    dismiss(id: number) {
      this.toasts = this.toasts.filter((t) => t.id !== id)
    },
  },
})
