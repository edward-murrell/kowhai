import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  it('renders markdown and strips scripts', () => {
    const html = renderMarkdown('**hi** <script>alert(1)</script> [x](javascript:alert(1))')
    expect(html).toContain('<strong>hi</strong>')
    expect(html).not.toContain('<script')
    expect(html).not.toContain('href="javascript')
  })
  it('marks mentions and safe links', () => {
    const html = renderMarkdown('ping @alice see https://example.com')
    expect(html).toContain('<span class="mention">@alice</span>')
    expect(html).toContain('rel="noopener noreferrer"')
  })
})
