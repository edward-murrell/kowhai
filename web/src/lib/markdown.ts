import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const md = new MarkdownIt({ html: false, linkify: true, breaks: true })

// Open links in a new tab without giving the target access to us.
const defaultLink = md.renderer.rules.link_open ?? ((t, i, o, _e, s) => s.renderToken(t, i, o))
md.renderer.rules.link_open = (tokens, idx, opts, env, self) => {
  tokens[idx].attrSet('target', '_blank')
  tokens[idx].attrSet('rel', 'noopener noreferrer')
  return defaultLink(tokens, idx, opts, env, self)
}

// @mentions get a span so they stand out.
function mentions(html: string): string {
  return html.replace(/(^|[\s(>])@([A-Za-z0-9][A-Za-z0-9._-]*[A-Za-z0-9]|[A-Za-z0-9])/g, '$1<span class="mention">@$2</span>')
}

export function renderMarkdown(src: string): string {
  const html = mentions(md.render(src ?? ''))
  return DOMPurify.sanitize(html, { ADD_ATTR: ['target'] })
}
