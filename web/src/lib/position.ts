// Fractional index keys, matching internal/position on the server. The
// server picks the real key. The client uses these only to sort an item
// in the right place before the server replies.
const DIGITS = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz'

export function between(a: string, b: string): string {
  if (b !== '') {
    let n = 0
    while (n < b.length && (a[n] ?? '0') === b[n]) n++
    if (n > 0) return b.slice(0, n) + between(a.slice(n), b.slice(n))
  }
  const da = a === '' ? 0 : DIGITS.indexOf(a[0])
  const db = b === '' ? DIGITS.length : DIGITS.indexOf(b[0])
  if (db - da > 1) return DIGITS[Math.floor((da + db + 1) / 2)]
  if (b !== '' && b.length > 1) return b.slice(0, 1)
  return DIGITS[da] + between(a.slice(1), '')
}

export function byPosition<T extends { position: string; id: string }>(a: T, b: T): number {
  if (a.position < b.position) return -1
  if (a.position > b.position) return 1
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
}
