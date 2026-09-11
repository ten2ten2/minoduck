import { formatMoneyExact } from '../utils/money'

export function useMoney() {
  const { locale } = useI18n()
  const language = computed(() =>
    locale.value === 'zh-hans' ? 'zh-CN' : locale.value === 'zh-hant' ? 'zh-TW' : 'en-US',
  )

  const money = (amount: string | null | undefined, currency: string) =>
    formatMoneyExact(amount, currency, language.value)

  const date = (value: string | null | undefined) =>
    value
      ? new Intl.DateTimeFormat(language.value, { dateStyle: 'medium', timeZone: 'UTC' }).format(
          new Date(value),
        )
      : '—'
  return { money, date, language }
}
