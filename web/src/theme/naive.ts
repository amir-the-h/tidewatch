// Naive UI theme overrides derived from the CSS tokens in src/styles/main.css.
// Naive computes hover/pressed shades from concrete colours, so it cannot take
// var(--x) directly; the values are read from the document once at startup.
import type { GlobalThemeOverrides } from 'naive-ui'

export function token(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(`--${name}`).trim()
}

// Mix a #rrggbb colour toward white (amount > 0) or black (amount < 0).
function shade(hex: string, amount: number): string {
  const target = amount > 0 ? 255 : 0
  const a = Math.abs(amount)
  const rgb = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16))
  return '#' + rgb.map(c => Math.round(c + (target - c) * a).toString(16).padStart(2, '0')).join('')
}

function status(color: string) {
  return { base: color, hover: shade(color, 0.12), pressed: shade(color, -0.12) }
}

export function buildNaiveOverrides(): GlobalThemeOverrides {
  const primary = status(token('primary'))
  const success = status(token('success'))
  const warning = status(token('warning'))
  const error = status(token('destructive'))
  const info = status(token('info'))
  const fontSans = token('font-sans')
  const fontMono = token('font-mono')

  return {
    common: {
      fontFamily: fontSans,
      fontFamilyMono: fontMono,
      fontSize: '13px',
      fontSizeMini: '11px',
      fontSizeTiny: '12px',
      fontSizeSmall: '13px',
      fontSizeMedium: '13px',
      lineHeight: '20px',
      heightTiny: '22px',
      heightSmall: token('control-sm'),
      heightMedium: token('control-md'),
      borderRadius: '6px',
      borderRadiusSmall: '4px',

      primaryColor: primary.base,
      primaryColorHover: primary.hover,
      primaryColorPressed: primary.pressed,
      primaryColorSuppl: primary.hover,
      successColor: success.base,
      successColorHover: success.hover,
      successColorPressed: success.pressed,
      successColorSuppl: success.hover,
      warningColor: warning.base,
      warningColorHover: warning.hover,
      warningColorPressed: warning.pressed,
      warningColorSuppl: warning.hover,
      errorColor: error.base,
      errorColorHover: error.hover,
      errorColorPressed: error.pressed,
      errorColorSuppl: error.hover,
      infoColor: info.base,
      infoColorHover: info.hover,
      infoColorPressed: info.pressed,
      infoColorSuppl: info.hover,

      baseColor: token('primary-foreground'),
      bodyColor: token('background'),
      cardColor: token('card'),
      modalColor: token('card'),
      popoverColor: token('popover'),
      tableColor: token('card'),
      tableHeaderColor: token('muted'),
      inputColor: token('background'),
      inputColorDisabled: token('muted'),
      actionColor: token('muted'),
      hoverColor: token('accent'),
      tableColorHover: token('accent'),
      borderColor: token('input'),
      dividerColor: token('border'),
      textColorBase: token('foreground'),
      textColor1: token('foreground'),
      textColor2: token('foreground'),
      textColor3: token('muted-foreground'),
      placeholderColor: token('muted-foreground'),
    },
    Button: {
      textColorPrimary: token('primary-foreground'),
      textColorHoverPrimary: token('primary-foreground'),
      textColorPressedPrimary: token('primary-foreground'),
      textColorFocusPrimary: token('primary-foreground'),
      textColorError: token('destructive-foreground'),
      textColorHoverError: token('destructive-foreground'),
      textColorPressedError: token('destructive-foreground'),
      textColorFocusError: token('destructive-foreground'),
      fontWeight: '500',
    },
    Card: {
      borderColor: token('border'),
      borderRadius: '8px',
    },
    DataTable: {
      borderColor: token('border'),
      thColor: token('muted'),
      thTextColor: token('muted-foreground'),
      thFontWeight: '500',
      tdColor: token('card'),
      tdColorHover: token('accent'),
    },
    Drawer: {
      color: token('card'),
    },
    // Naive's dark inputs are borderless fills; the standard wants an `input` hairline.
    Input: {
      border: `1px solid ${token('input')}`,
      borderHover: `1px solid ${primary.hover}`,
      borderFocus: `1px solid ${primary.base}`,
    },
    InternalSelection: {
      border: `1px solid ${token('input')}`,
      borderHover: `1px solid ${primary.hover}`,
      borderActive: `1px solid ${primary.base}`,
      borderFocus: `1px solid ${primary.base}`,
    },
    Tooltip: {
      color: token('popover'),
      textColor: token('popover-foreground'),
    },
  }
}
