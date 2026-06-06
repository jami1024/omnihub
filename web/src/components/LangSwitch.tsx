import { useI18n } from '../lib/i18n'

// LangSwitch toggles between English and Simplified Chinese. The button
// shows the language it will switch TO (中文 when on EN, EN when on 中文),
// matching the theme-toggle affordance in the header.
export function LangSwitch({ className = '' }: { className?: string }) {
  const { lang, setLang, t } = useI18n()
  return (
    <button
      type="button"
      onClick={() => setLang(lang === 'en' ? 'zh' : 'en')}
      title={t('lang.label')}
      aria-label={t('lang.label')}
      className={
        'inline-flex h-10 min-w-10 items-center justify-center rounded-lg border border-line px-3 text-xs font-medium text-muted transition-colors hover:text-ink ' +
        className
      }
    >
      {lang === 'en' ? t('lang.switchToZh') : t('lang.switchToEn')}
    </button>
  )
}
