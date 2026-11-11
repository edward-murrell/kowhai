import type { Activity } from '../api/client'

// describe turns an activity row into a short sentence, without the actor.
export function describe(a: Activity): string {
  const p = a.payload as Record<string, any>
  const card = p.card_title ? `“${p.card_title}”` : 'a card'
  switch (a.type) {
    case 'workspace.created':
      return `created the workspace ${p.name}`
    case 'workspace.updated':
      return `updated the workspace`
    case 'workspace.member_added':
      return p.via === 'invite'
        ? `joined ${p.workspace_name ?? 'the workspace'} from an invite`
        : `added ${p.user_name} to ${p.workspace_name ?? 'the workspace'} as ${p.role}`
    case 'workspace.member_removed':
      return `removed ${p.user_name} from the workspace`
    case 'workspace.invited':
      return `invited ${p.email} as ${p.role}`
    case 'board.created':
      return p.from_board_name ? `created this board from ${p.from_board_name}` : `created the board ${p.name}`
    case 'board.updated':
      return `updated the board`
    case 'board.deleted':
      return `deleted the board ${p.name}`
    case 'board.member_added':
      return `added ${p.user_name} to ${p.board_name ?? 'the board'} as ${p.role}`
    case 'board.member_removed':
      return `removed ${p.user_name} from the board`
    case 'label.created':
      return `created a label`
    case 'label.updated':
      return `changed a label`
    case 'label.deleted':
      return `deleted a label`
    case 'list.created':
      return `added the list ${p.name}`
    case 'list.updated':
      if (p.changes?.archived === true) return `archived the list ${p.name}`
      if (p.changes?.archived === false) return `restored the list ${p.name}`
      if (p.changes?.name) return `renamed the list ${p.previous_name ?? ''} to ${p.name}`
      return `changed the list ${p.name}`
    case 'list.moved':
      return `moved the list ${p.name}`
    case 'card.created':
      if (p.recurrence_id) return `made ${card} from a repeating card`
      if (p.from_template) return `made ${card} from a template in ${p.list_name}`
      return `added ${card} to ${p.list_name}`
    case 'card.updated': {
      const c = p.changes ?? {}
      if (c.archived === true) return `archived ${card}`
      if (c.archived === false) return `restored ${card}`
      if (c.title) return `renamed ${c.previous_title ?? 'a card'} to “${c.title}”`
      if (c.due_done === true) return `marked the due date on ${card} complete`
      if ('due_at' in c) return c.due_at ? `set ${card} due ${new Date(c.due_at).toLocaleString()}` : `removed the due date from ${card}`
      if (c.description) return `changed the description of ${card}`
      return `updated ${card}`
    }
    case 'card.moved':
      if (p.reason === 'status') return `moved ${card} to ${p.to_list_name} because Claude is ${p.status}`
      return p.from_list_id === p.to_list_id ? `moved ${card} in ${p.to_list_name}` : `moved ${card} from ${p.from_list_name} to ${p.to_list_name}`
    case 'card.deleted':
      return `deleted ${card}`
    case 'card.member_added':
      return `added ${p.user_name} to ${card}`
    case 'card.member_removed':
      return `removed ${p.user_name} from ${card}`
    case 'card.label_added':
      return `added the ${p.label_name || p.label_colour} label to ${card}`
    case 'card.label_removed':
      return `removed the ${p.label_name || p.label_colour} label from ${card}`
    case 'card.due_soon':
      return `${card} is due within 24 hours`
    case 'card.overdue':
      return `${card} is overdue`
    case 'checklist.created':
      return `added the checklist ${p.title} to ${card}`
    case 'checklist.updated':
      return `changed the checklist ${p.title} on ${card}`
    case 'checklist.deleted':
      return `removed the checklist ${p.title} from ${card}`
    case 'checklist_item.created':
      return `added “${p.text}” to ${card}`
    case 'checklist_item.updated':
      if (p.changes?.done === true) return `completed “${p.text}” on ${card}`
      if (p.changes?.done === false) return `marked “${p.text}” incomplete on ${card}`
      return `changed “${p.text}” on ${card}`
    case 'checklist_item.deleted':
      return `removed “${p.text}” from ${card}`
    case 'comment.created':
      return `commented on ${card}: ${p.excerpt ?? ''}`
    case 'comment.updated':
      return `edited a comment on ${card}`
    case 'comment.deleted':
      return `deleted a comment on ${card}`
    case 'attachment.added':
      return `attached ${p.filename} to ${card}`
    case 'attachment.deleted':
      return `removed ${p.filename} from ${card}`
    case 'link.created':
      return p.kind === 'claude_code' ? `linked a Claude Code session to ${card}` : `added a link to ${card}`
    case 'link.status': {
      const where = [p.repo, p.branch].filter(Boolean).join(' @ ')
      const s = { working: 'is working', waiting: 'is waiting for you', idle: 'finished its turn', ended: 'ended', stale: 'went quiet' }[p.status as string] ?? p.status
      return `Claude session ${p.session ?? ''}${where ? ` (${where})` : ''} ${s} on ${card}${p.detail ? `: ${p.detail}` : ''}`
    }
    case 'link.deleted':
      return `removed a link from ${card}`
    case 'recurrence.created':
      return `made ${card} repeat`
    case 'recurrence.updated':
      return p.paused ? `paused the repeat for ${card}` : `changed the repeat for ${card}`
    case 'recurrence.deleted':
      return `stopped a repeating card`
    case 'recurrence.paused':
      return `the repeat for ${card} was paused: ${p.reason}`
    default:
      return a.type
  }
}

// actorName is the name shown before the sentence.
export function actorName(a: Activity): string {
  if (!a.actor) return 'Kōwhai'
  return a.actor_label ? `${a.actor.display_name} (${a.actor_label})` : a.actor.display_name
}
