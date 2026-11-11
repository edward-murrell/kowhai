import { describe, expect, it } from 'vitest'
import { between } from './position'

describe('between', () => {
  it('matches the server for simple cases', () => {
    expect(between('', '')).toBe('V')
    expect(between('V', '')).toBe('l')
    expect(between('', 'V')).toBe('G')
  })
  it('always sorts between its inputs', () => {
    const keys = ['V']
    for (let i = 0; i < 500; i++) {
      const j = (i * 7919) % (keys.length + 1)
      const k = between(keys[j - 1] ?? '', keys[j] ?? '')
      keys.splice(j, 0, k)
    }
    const sorted = [...keys].sort()
    expect(keys).toEqual(sorted)
    expect(new Set(keys).size).toBe(keys.length)
  })
})
