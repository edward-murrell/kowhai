import { defineStore } from 'pinia'
import {
  api,
  call,
  type Activity,
  type Board,
  type Card,
  type CardLink,
  type Label,
  type List,
  type UserRef,
} from '../api/client'
import { between, byPosition } from '../lib/position'
import { useSession } from './session'

const STATUS_RANK: Record<string, number> = { waiting: 4, working: 3, idle: 2, stale: 1, ended: 0 }

function upsert<T extends { id: string; version?: number }>(arr: T[], item: T): void {
  const i = arr.findIndex((x) => x.id === item.id)
  if (i < 0) arr.push(item)
  else if (item.version === undefined || (arr[i].version ?? 0) <= item.version) arr[i] = item
}

export const useBoard = defineStore('board', {
  state: () => ({
    board: null as Board | null,
    lists: [] as List[],
    cards: [] as Card[],
    labels: [] as Label[],
    users: [] as UserRef[],
    links: [] as CardLink[],
    lastEventId: '',
    showArchived: false,
    stream: null as EventSource | null,
    // Bumped when something on a card changes, so an open card reloads.
    cardTicks: {} as Record<string, number>,
    // Recent activity for the board sidebar.
    feed: [] as Activity[],
    connected: false,
  }),
  getters: {
    sortedLists: (s) => [...s.lists].filter((l) => s.showArchived || !l.archived).sort(byPosition),
    cardsIn: (s) => (listId: string) =>
      s.cards.filter((c) => c.list_id === listId && (s.showArchived || !c.archived)).sort(byPosition),
    labelById: (s) => (id: string) => s.labels.find((l) => l.id === id),
    userById: (s) => (id: string) => s.users.find((u) => u.id === id),
    canEdit: (s) => s.board?.role === 'admin' || s.board?.role === 'editor',
    isAdmin: (s) => s.board?.role === 'admin',
    // cardStatus is the most urgent Claude Code status on a card.
    cardSessions: (s) => (cardId: string) => {
      const ls = s.links.filter((l) => l.card_id === cardId && l.kind === 'claude_code' && l.status && l.status !== 'ended')
      let top: CardLink | undefined
      for (const l of ls) if (!top || STATUS_RANK[l.status!] > STATUS_RANK[top.status!]) top = l
      return { top, count: ls.length }
    },
  },
  actions: {
    async load(id: string) {
      if (this.board?.id !== id) this.close()
      const full = await call(
        api.GET('/boards/{id}/full', { params: { path: { id }, query: { archived: this.showArchived } } }),
      )
      this.board = full.board
      this.lists = full.lists
      this.cards = full.cards
      this.labels = full.labels
      this.users = full.users
      this.links = full.links
      this.lastEventId = full.last_event_id ?? ''
      this.listen()
    },
    listen() {
      if (this.stream || !this.board) return
      const q = this.lastEventId ? `?last_event_id=${encodeURIComponent(this.lastEventId)}` : ''
      const es = new EventSource(`/api/v1/boards/${this.board.id}/events${q}`)
      es.onopen = () => (this.connected = true)
      es.onerror = () => (this.connected = false)
      es.addEventListener('activity', (e) => {
        const me = e as MessageEvent
        if (me.lastEventId) this.lastEventId = me.lastEventId
        this.apply(JSON.parse(me.data) as Activity)
      })
      es.addEventListener('link', (e) => {
        upsert(this.links, JSON.parse((e as MessageEvent).data) as CardLink)
      })
      es.addEventListener('resync', () => {
        if (this.board) void this.reload()
      })
      this.stream = es
    },
    async reload() {
      if (!this.board) return
      this.close()
      await this.load(this.board.id)
    },
    close() {
      this.stream?.close()
      this.stream = null
      this.connected = false
    },
    tick(cardId?: string | null) {
      if (cardId) this.cardTicks[cardId] = (this.cardTicks[cardId] ?? 0) + 1
    },
    // apply patches local state from one activity event.
    apply(a: Activity) {
      this.feed.unshift(a)
      if (this.feed.length > 100) this.feed.pop()
      const p = a.payload as Record<string, any>
      const entity = p.entity
      const [kind] = a.type.split('.')
      switch (kind) {
        case 'card':
          if (a.type === 'card.deleted') {
            this.cards = this.cards.filter((c) => c.id !== p.card_id)
          } else if (entity && !entity.is_template) {
            upsert(this.cards, entity as Card)
          } else if (entity?.is_template) {
            this.cards = this.cards.filter((c) => c.id !== entity.id)
          }
          break
        case 'list':
          if (entity) upsert(this.lists, entity as List)
          break
        case 'label':
          if (a.type === 'label.deleted') {
            this.labels = this.labels.filter((l) => l.id !== p.label_id)
            for (const c of this.cards) c.label_ids = c.label_ids.filter((id) => id !== p.label_id)
          } else if (entity) upsert(this.labels, entity as Label)
          break
        case 'link':
          if (a.type === 'link.deleted') this.links = this.links.filter((l) => l.id !== p.link_id)
          else if (entity) upsert(this.links, entity as CardLink)
          break
        case 'board':
          if (a.type === 'board.updated' && entity && this.board) {
            this.board = { ...(entity as Board), role: this.board.role }
          } else if (a.type.startsWith('board.member')) {
            void this.reload()
          }
          break
      }
      if (p.card && !p.card.is_template) upsert(this.cards, p.card as Card)
      this.tick(a.card_id)
    },

    // ---- Lists ----
    async addList(name: string) {
      const last = this.sortedLists[this.sortedLists.length - 1]
      const l = await this.run(api.POST('/boards/{id}/lists', { params: { path: { id: this.board!.id } }, body: { name, after_id: last?.id } }))
      if (l) upsert(this.lists, l)
    },
    async updateList(id: string, body: { name?: string; archived?: boolean }) {
      const l = await this.run(api.PATCH('/lists/{id}', { params: { path: { id } }, body }))
      if (l) upsert(this.lists, l)
    },
    async moveList(id: string, newIndex: number) {
      const others = this.sortedLists.filter((l) => l.id !== id)
      const after = others[newIndex - 1]
      const before = others[newIndex]
      const list = this.lists.find((l) => l.id === id)!
      list.position = between(after?.position ?? '', before?.position ?? '')
      const l = await this.run(api.POST('/lists/{id}/move', { params: { path: { id } }, body: { after_id: after?.id, before_id: before?.id } }))
      if (l) upsert(this.lists, l)
    },

    // ---- Cards ----
    async addCard(listId: string, title: string) {
      const cards = this.cardsIn(listId)
      const last = cards[cards.length - 1]
      const c = await this.run(api.POST('/lists/{id}/cards', { params: { path: { id: listId } }, body: { title, after_id: last?.id } }))
      if (c) upsert(this.cards, c)
      return c
    },
    // moveCard puts a card at newIndex in a list. The UI moves first and
    // the server confirms. On failure the board is re-fetched.
    async moveCard(id: string, listId: string, newIndex: number) {
      const others = this.cardsIn(listId).filter((c) => c.id !== id)
      const after = others[newIndex - 1]
      const before = others[newIndex]
      const card = this.cards.find((c) => c.id === id)
      if (!card) return
      card.list_id = listId
      card.position = between(after?.position ?? '', before?.position ?? '')
      const c = await this.run(
        api.POST('/cards/{id}/move', { params: { path: { id } }, body: { list_id: listId, after_id: after?.id, before_id: before?.id } }),
      )
      if (c) upsert(this.cards, c)
    },
    async updateCard(id: string, body: Record<string, unknown>) {
      const card = this.cards.find((c) => c.id === id)
      if (card) Object.assign(card, body)
      const c = await this.run(api.PATCH('/cards/{id}', { params: { path: { id }, header: { 'If-Match': card ? String(card.version) : undefined } }, body: body as any }))
      if (c) upsert(this.cards, c)
      return c
    },

    // run calls the API. Errors show a toast and re-fetch the board, which
    // rolls back any optimistic change.
    async run<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T | undefined> {
      try {
        return await call(p)
      } catch (e) {
        useSession().error(e)
        await this.reload()
        return undefined
      }
    },
  },
})
