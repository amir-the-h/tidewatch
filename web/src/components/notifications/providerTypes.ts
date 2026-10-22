import type { NotificationProvider } from '../../api/notifications'

export type ProviderType = NotificationProvider['type']

export const providerTypeOptions: { label: string; value: ProviderType }[] = [
  { label: 'Pushover', value: 'pushover' },
  { label: 'Telegram', value: 'telegram' },
  { label: 'Discord',  value: 'discord'  },
]

export const providerTypeLabel = Object.fromEntries(
  providerTypeOptions.map(o => [o.value, o.label]),
) as Record<ProviderType, string>
