import { differenceInHours, format, formatDistanceToNowStrict, isPast, isSameYear } from 'date-fns'
import { enNZ } from 'date-fns/locale'

export function shortDate(iso: string): string {
  const d = new Date(iso)
  return format(d, isSameYear(d, new Date()) ? 'd MMM' : 'd MMM yyyy', { locale: enNZ })
}

export function longDate(iso: string): string {
  return format(new Date(iso), 'd MMM yyyy, h:mm a', { locale: enNZ })
}

export function ago(iso: string): string {
  return formatDistanceToNowStrict(new Date(iso), { addSuffix: true, locale: enNZ })
}

// dueState drives the colour of due date badges.
export function dueState(iso: string | undefined, done: boolean): 'done' | 'overdue' | 'soon' | 'later' | '' {
  if (!iso) return ''
  if (done) return 'done'
  const d = new Date(iso)
  if (isPast(d)) return 'overdue'
  if (differenceInHours(d, new Date()) < 24) return 'soon'
  return 'later'
}

// toLocalInput formats a date for <input type="datetime-local">.
export function toLocalInput(iso?: string): string {
  if (!iso) return ''
  return format(new Date(iso), "yyyy-MM-dd'T'HH:mm")
}

export function fromLocalInput(v: string): string | null {
  return v ? new Date(v).toISOString() : null
}
