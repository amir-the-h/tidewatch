// Markdown → sanitised HTML for LLM output. DOMPurify strips script, event handlers and
// javascript: URLs, so the result is safe for v-html. Links open in a new tab. Images are
// dropped: the LLM reads untrusted tweet text, and an injected ![](https://…?d=…) would make
// the browser send data to that URL with no click.
import { marked } from 'marked'
import DOMPurify from 'dompurify'

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer')
  }
})

export function renderMarkdown(source: string): string {
  return DOMPurify.sanitize(marked.parse(source, { async: false, gfm: true, breaks: true }), {
    FORBID_TAGS: ['img', 'picture', 'source', 'video', 'audio'],
  })
}
