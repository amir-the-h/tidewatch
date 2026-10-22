<!-- Renders LLM-written markdown (digests, chat answers) as sanitised HTML; see utils/markdown.ts. -->
<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '../utils/markdown'

const props = defineProps<{ source: string }>()

const html = computed(() => renderMarkdown(props.source))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- sanitised above -->
  <div class="markdown" v-html="html" />
</template>

<style scoped>
.markdown { line-height: 1.6; overflow-wrap: anywhere; }
.markdown :deep(> :first-child) { margin-top: 0; }
.markdown :deep(> :last-child) { margin-bottom: 0; }
.markdown :deep(p),
.markdown :deep(ul),
.markdown :deep(ol),
.markdown :deep(pre),
.markdown :deep(blockquote),
.markdown :deep(table) { margin: 0 0 0.75em; }
.markdown :deep(h1),
.markdown :deep(h2),
.markdown :deep(h3),
.markdown :deep(h4) { margin: 1.25em 0 0.5em; font-weight: 600; line-height: 1.3; }
.markdown :deep(h1) { font-size: var(--text-title); }
.markdown :deep(h2) { font-size: var(--text-title); }
.markdown :deep(h3),
.markdown :deep(h4) { font-size: var(--text-body); }
.markdown :deep(ul),
.markdown :deep(ol) { padding-left: 1.5em; }
/* Tailwind's preflight removes list markers; restore them. */
.markdown :deep(ul) { list-style-type: disc; }
.markdown :deep(ol) { list-style-type: decimal; }
.markdown :deep(ul ul) { list-style-type: circle; }
.markdown :deep(li::marker) { color: var(--muted-foreground); }
.markdown :deep(li) { margin: 0.2em 0; }
.markdown :deep(li > ul),
.markdown :deep(li > ol) { margin: 0.2em 0 0; }
.markdown :deep(strong) { font-weight: 600; }
.markdown :deep(a) { color: var(--primary); text-decoration: underline; text-underline-offset: 2px; }
.markdown :deep(code) { font-family: var(--font-mono); font-size: 0.92em; background: var(--muted); border-radius: 4px; padding: 0.1em 0.35em; }
.markdown :deep(pre) { background: var(--muted); border-radius: 6px; padding: 0.75em 1em; overflow-x: auto; }
.markdown :deep(pre code) { background: transparent; padding: 0; }
.markdown :deep(blockquote) { border-left: 2px solid var(--border); padding-left: 0.75em; color: var(--muted-foreground); }
.markdown :deep(hr) { border: 0; border-top: 1px solid var(--border); margin: 1em 0; }
.markdown :deep(table) { border-collapse: collapse; display: block; overflow-x: auto; }
.markdown :deep(th),
.markdown :deep(td) { border: 1px solid var(--border); padding: 0.35em 0.6em; text-align: left; }
.markdown :deep(th) { background: var(--muted); font-weight: 600; }
</style>
