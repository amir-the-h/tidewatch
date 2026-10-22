// Toasts for completed actions and errors. Wraps Naive's useMessage today (vue-sonner later).
// See docs/ui-standard.md → Feedback.
import { useMessage } from 'naive-ui'

export function useToast() {
  const message = useMessage()
  return {
    success: (text: string) => { message.success(text) },
    error: (text: string) => { message.error(text) },
    info: (text: string) => { message.info(text) },
  }
}
