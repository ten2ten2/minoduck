import en from '../../../packages/locales/en.json'
import hans from '../../../packages/locales/zh-hans.json'
import hant from '../../../packages/locales/zh-hant.json'
export default defineI18nConfig(() => ({
  legacy: false,
  fallbackLocale: 'en',
  messages: { en, 'zh-hans': hans, 'zh-hant': hant },
}))
