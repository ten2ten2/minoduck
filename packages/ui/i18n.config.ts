import en from '../locales/en.json'
import hans from '../locales/zh-hans.json'
import hant from '../locales/zh-hant.json'

export default defineI18nConfig(() => ({
  legacy: false,
  fallbackLocale: 'en',
  messages: { en, 'zh-hans': hans, 'zh-hant': hant },
}))
