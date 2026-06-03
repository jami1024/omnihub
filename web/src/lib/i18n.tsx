// Lightweight i18n: a language context + t() backed by flat en/zh
// dictionaries, persisted in localStorage. No external dependency —
// mirrors the existing theme.ts approach. Components call useI18n() and
// wrap user-facing strings in t('namespace.key'). Missing keys fall back
// to English, then to the key itself, so the app never crashes on a gap.
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import en from './locales/en'
import zh from './locales/zh'

export type Lang = 'en' | 'zh'

const DICTS: Record<Lang, Record<string, string>> = { en, zh }
const KEY = 'omnihub.lang'

function initialLang(): Lang {
  const stored = localStorage.getItem(KEY)
  if (stored === 'en' || stored === 'zh') return stored
  return navigator.language?.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

type TFunc = (key: string, vars?: Record<string, string | number>) => string

interface I18nValue {
  lang: Lang
  setLang: (l: Lang) => void
  t: TFunc
}

const I18nContext = createContext<I18nValue | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(initialLang)

  useEffect(() => {
    document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en'
  }, [lang])

  function setLang(l: Lang) {
    localStorage.setItem(KEY, l)
    setLangState(l)
  }

  const t: TFunc = (key, vars) => {
    let s = DICTS[lang][key] ?? DICTS.en[key] ?? key
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        s = s.split(`{${k}}`).join(String(v))
      }
    }
    return s
  }

  return <I18nContext.Provider value={{ lang, setLang, t }}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nValue {
  const v = useContext(I18nContext)
  if (!v) {
    throw new Error('useI18n must be used within I18nProvider')
  }
  return v
}
