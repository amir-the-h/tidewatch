<!-- Chat composer: autosizing message field with Send / Stop. Enter sends, Shift+Enter adds a line,
     and Enter while an IME is composing confirms the composition instead of sending. -->
<script setup lang="ts">
import { ref } from 'vue'
import { SendHorizontalIcon, SquareIcon } from '@lucide/vue'
import { IconButton, Textarea } from '@/components/ui'

const props = defineProps<{
  disabled?: boolean
  streaming?: boolean
}>()

const emit = defineEmits<{ send: [content: string]; stop: [] }>()

const text  = ref('')
const field = ref<{ focus: () => void } | null>(null)

function send() {
  const content = text.value.trim()
  if (!content || props.disabled || props.streaming) return
  text.value = ''
  emit('send', content)
}

function onKeydown(e: KeyboardEvent) {
  // keyCode 229 covers browsers that report the IME's confirming Enter without isComposing.
  if (e.key !== 'Enter' || e.shiftKey || e.isComposing || e.keyCode === 229) return
  e.preventDefault()
  send()
}

defineExpose({ focus: () => field.value?.focus() })
</script>

<template>
  <div class="flex shrink-0 items-end gap-2 border-t border-border pt-3">
    <div class="min-w-0 flex-1">
      <label for="chat-message" class="sr-only">Message</label>
      <Textarea
        id="chat-message"
        ref="field"
        v-model="text"
        autosize
        :rows="1"
        :max-rows="5"
        placeholder="Ask about your Tidewatch data. Enter sends, Shift+Enter adds a line."
        :disabled="disabled || streaming"
        @keydown="onKeydown"
      />
    </div>
    <IconButton
      v-if="!streaming"
      :icon="SendHorizontalIcon"
      label="Send (Enter)"
      variant="default"
      :disabled="disabled || !text.trim()"
      @click="send"
    />
    <IconButton v-else :icon="SquareIcon" label="Stop generating" variant="destructive" @click="$emit('stop')" />
  </div>
</template>
