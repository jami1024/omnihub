// Theme preference: 'system' follows the OS, 'light'/'dark' override it.
// The class is set on <html> before paint by an inline script in
// index.html; this module keeps it in sync after boot.
export type Theme = 'system' | 'light' | 'dark'

const KEY = 'omnihub.theme'

export function getTheme(): Theme {
  const t = localStorage.getItem(KEY)
  return t === 'light' || t === 'dark' ? t : 'system'
}

export function resolvedDark(t: Theme): boolean {
  return t === 'dark' || (t === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
}

export function applyTheme(t: Theme) {
  document.documentElement.classList.toggle('dark', resolvedDark(t))
}

export function setTheme(t: Theme) {
  if (t === 'system') localStorage.removeItem(KEY)
  else localStorage.setItem(KEY, t)
  applyTheme(t)
}

// Cycle order matches the toggle button: system → light → dark → system.
export function nextTheme(t: Theme): Theme {
  return t === 'system' ? 'light' : t === 'light' ? 'dark' : 'system'
}
