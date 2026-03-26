// Helpers for rendering icon-only action buttons with tooltips in h() render functions
import { h } from 'vue'
import { NButton, NIcon, NTooltip, NPopconfirm } from 'naive-ui'
import type { Component } from 'vue'

interface BtnOpts {
  type?: 'default' | 'primary' | 'info' | 'success' | 'warning' | 'error'
  ghost?: boolean
  disabled?: boolean
  loading?: boolean
}

/** Icon-only button with hover tooltip */
export function iconBtn(
  icon: Component,
  label: string,
  onClick: () => void,
  opts: BtnOpts = {},
) {
  return h(NTooltip, { trigger: 'hover', delay: 200 }, {
    trigger: () => h(
      NButton,
      { size: 'small', type: opts.type, ghost: opts.ghost, disabled: opts.disabled, loading: opts.loading, onClick },
      { icon: () => h(NIcon, null, { default: () => h(icon as any) }) },
    ),
    default: () => label,
  })
}

/** Icon-only button with hover tooltip + popconfirm on click */
export function iconBtnConfirm(
  icon: Component,
  label: string,
  confirmText: string,
  onConfirm: () => void,
  opts: BtnOpts = {},
) {
  return h(NTooltip, { trigger: 'hover', delay: 200 }, {
    trigger: () => h(NPopconfirm, { onPositiveClick: onConfirm }, {
      trigger: () => h(
        NButton,
        { size: 'small', type: opts.type, ghost: opts.ghost, disabled: opts.disabled },
        { icon: () => h(NIcon, null, { default: () => h(icon as any) }) },
      ),
      default: () => confirmText,
    }),
    default: () => label,
  })
}
