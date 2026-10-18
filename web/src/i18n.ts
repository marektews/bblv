import { createI18n } from 'vue-i18n'
import en from './locales/en'
import pl from './locales/pl'

export type Locale = 'pl' | 'en'

function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem('lang')
    if (saved === 'pl' || saved === 'en') {
      return saved
    }
  } catch {
    // Storage may be unavailable, e.g. in private windows.
  }
  return navigator.language.toLowerCase().startsWith('pl') ? 'pl' : 'en'
}

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(),
  fallbackLocale: 'en',
  messages: { pl, en },
})
document.documentElement.lang = i18n.global.locale.value

export function setLocale(locale: Locale) {
  i18n.global.locale.value = locale
  document.documentElement.lang = locale
  try {
    localStorage.setItem('lang', locale)
  } catch {
    // Not persisted; the browser language is used next time.
  }
}
