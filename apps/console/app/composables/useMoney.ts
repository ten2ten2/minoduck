export function useMoney() {
  const { locale } = useI18n()
  const language = computed(() =>
    locale.value === 'zh-hans' ? 'zh-CN' : locale.value === 'zh-hant' ? 'zh-TW' : 'en-US',
  )

  const money = (amount: string | null | undefined, currency: string) => {
    if (amount == null) return '—'
    const match = amount.match(/^(-?)(\d+)(?:\.(\d+))?$/)
    if (!match) return amount

    const sign = match[1] ?? ''
    const integer = match[2]!
    const rawFraction = match[3] ?? ''
    const formatter = new Intl.NumberFormat(language.value, {
      style: 'currency',
      currency,
      currencyDisplay: 'code',
      minimumFractionDigits: 0,
      maximumFractionDigits: 0,
    })
    const parts = formatter.formatToParts(BigInt(integer))
    const trimmed = rawFraction.replace(/0+$/, '')
    const fraction = trimmed.padEnd(2, '0')
    if (fraction) {
      const lastNumberPart = parts.findLastIndex(
        (part) => part.type === 'integer' || part.type === 'group',
      )
      const decimal =
        new Intl.NumberFormat(language.value)
          .formatToParts(1.1)
          .find((part) => part.type === 'decimal')?.value ?? '.'
      parts.splice(
        lastNumberPart + 1,
        0,
        { type: 'decimal', value: decimal },
        { type: 'fraction', value: fraction },
      )
    }
    return (sign ? '-' : '') + parts.map((part) => part.value).join('')
  }

  const date = (value: string | null | undefined) =>
    value
      ? new Intl.DateTimeFormat(language.value, { dateStyle: 'medium', timeZone: 'UTC' }).format(
          new Date(value),
        )
      : '—'
  return { money, date, language }
}
