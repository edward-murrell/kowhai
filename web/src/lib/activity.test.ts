import { describe, expect, it } from 'vitest'
import { actorName, describe as d } from './activity'

const base = { id: '1', workspace_id: 'w', actor_label: '', created_at: '2026-01-01T00:00:00Z' }

describe('activity text', () => {
  it('describes moves', () => {
    expect(d({ ...base, type: 'card.moved', payload: { card_title: 'X', from_list_id: 'a', from_list_name: 'To do', to_list_id: 'b', to_list_name: 'Done' } })).toBe(
      'moved “X” from To do to Done',
    )
  })
  it('names labelled actors', () => {
    expect(actorName({ ...base, type: 'x', payload: {}, actor: { id: 'u', display_name: 'Alice', avatar_url: '' }, actor_label: 'Claude' })).toBe('Alice (Claude)')
    expect(actorName({ ...base, type: 'x', payload: {} })).toBe('Kōwhai')
  })
})
