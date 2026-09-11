export function useAccountPreferences() {
  const { api, user, errorText } = useApi()
  const error = ref('')

  const save = async (locale: string, theme: string) => {
    if (!user.value) return
    error.value = ''
    try {
      await api('/me/preferences', { method: 'PATCH', body: { locale, theme } })
      user.value.locale = locale
      user.value.theme = theme
      user.value.locale_explicit = true
    } catch (cause) {
      error.value = errorText(cause)
    }
  }

  return { preferenceError: readonly(error), savePreferences: save }
}
