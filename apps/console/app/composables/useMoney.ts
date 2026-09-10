// Intl accepts decimal strings at runtime (ECMA-402), preserving precision.
export function useMoney() {
  const { locale } = useI18n()
  const language = computed(() =>
    locale.value === 'zh-hans' ? 'zh-CN' : locale.value === 'zh-hant' ? 'zh-TW' : 'en-US',
  )
  const money = (amount: string | null | undefined, currency: string) =>
    amount == null
      ? '—'
      : new Intl.NumberFormat(language.value, {
          style: 'currency',
          currency,
          currencyDisplay: 'code',
          minimumFractionDigits: 2,
          maximumFractionDigits: 6,
        }).format(amount as unknown as number)
  const date = (value: string | undefined) =>
    value
      ? new Intl.DateTimeFormat(language.value, { dateStyle: 'medium', timeZone: 'UTC' }).format(
          new Date(value),
        )
      : '—'
  return { money, date, language }
}
