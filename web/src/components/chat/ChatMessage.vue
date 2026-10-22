<!-- One chat turn. User messages are plain text; assistant answers are LLM markdown, preceded by the
     tool calls the assistant made, each a collapsible <details> with its arguments and (truncated) result. -->
<script setup lang="ts">
import { ChevronRightIcon, WrenchIcon } from '@lucide/vue'
import MarkdownContent from '../MarkdownContent.vue'
import type { DisplayMessage } from '../../stores/chat'

const props = defineProps<{
  message: DisplayMessage
  /** This is the reply currently streaming in. */
  streaming?: boolean
}>()

const RESULT_MAX = 500

function formatArgs(args: Record<string, unknown>): string {
  return JSON.stringify(args, null, 2)
}

function truncateResult(result: string): string {
  return result.length > RESULT_MAX ? result.slice(0, RESULT_MAX) + '…' : result
}

const isUser = props.message.role === 'user'
</script>

<template>
  <div class="flex" :class="isUser ? 'justify-end' : 'justify-start'">
    <div class="flex max-w-[80%] min-w-0 flex-col gap-1.5">
      <template v-if="!isUser">
        <details
          v-for="(step, si) in message.toolSteps"
          :key="si"
          class="group rounded-md border border-border bg-muted/40 px-3 py-1.5 text-small text-muted-foreground"
        >
          <summary class="flex cursor-pointer list-none items-center gap-1.5 select-none [&::-webkit-details-marker]:hidden">
            <ChevronRightIcon :size="14" :stroke-width="1.75" class="shrink-0 transition-transform group-open:rotate-90 motion-reduce:transition-none" />
            <WrenchIcon :size="14" :stroke-width="1.75" class="shrink-0 text-info" />
            <span>Called <code class="font-mono text-foreground">{{ step.name }}</code></span>
          </summary>
          <div class="mt-2 flex flex-col gap-2 pb-1">
            <div>
              <div class="text-label uppercase">Arguments</div>
              <pre class="m-0 mt-1 font-mono text-small whitespace-pre-wrap break-all text-foreground">{{ formatArgs(step.args) }}</pre>
            </div>
            <div>
              <div class="text-label uppercase">Result</div>
              <pre class="m-0 mt-1 font-mono text-small whitespace-pre-wrap break-all text-foreground">{{ truncateResult(step.result) }}</pre>
            </div>
          </div>
        </details>
      </template>

      <div
        v-if="isUser"
        class="rounded-lg rounded-br-sm border border-primary/30 bg-primary/15 px-3 py-2 whitespace-pre-wrap break-words"
      >{{ message.content }}</div>

      <div
        v-else-if="message.content || streaming"
        class="rounded-lg rounded-bl-sm border border-border bg-card px-3 py-2"
        :class="{ 'streaming-cursor': streaming && message.content }"
      >
        <MarkdownContent v-if="message.content" :source="message.content" />
        <div v-else role="status" class="flex h-5 items-center gap-1">
          <span class="sr-only">Assistant is responding</span>
          <span
            v-for="d in ['[animation-delay:0ms]', '[animation-delay:150ms]', '[animation-delay:300ms]']"
            :key="d"
            class="size-1.5 animate-pulse rounded-full bg-muted-foreground motion-reduce:animate-none"
            :class="d"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Blinking caret after the last streamed text; sits after the last block (or last list item). */
.streaming-cursor :deep(.markdown > :last-child:not(ul, ol))::after,
.streaming-cursor :deep(.markdown > :is(ul, ol):last-child > li:last-child)::after {
  content: '';
  display: inline-block;
  width: 2px;
  height: 1em;
  margin-left: 2px;
  vertical-align: text-bottom;
  background: var(--primary);
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  50% { opacity: 0; }
}

@media (prefers-reduced-motion: reduce) {
  .streaming-cursor :deep(.markdown > :last-child:not(ul, ol))::after,
  .streaming-cursor :deep(.markdown > :is(ul, ol):last-child > li:last-child)::after { animation: none; }
}
</style>
