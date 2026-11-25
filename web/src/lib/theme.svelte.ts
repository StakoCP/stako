export type Theme = 'light' | 'dark';

const STORAGE_KEY = 'stako_theme';

function detectInitialTheme(): Theme {
  if (typeof window === 'undefined') return 'light';
  const saved = window.localStorage.getItem(STORAGE_KEY);
  if (saved === 'light' || saved === 'dark') return saved;
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(theme: Theme) {
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', theme);
  }
}

let currentTheme = $state<Theme>(detectInitialTheme());
applyTheme(currentTheme);

export const themeStore = {
  get current(): Theme {
    return currentTheme;
  },
  toggle() {
    currentTheme = currentTheme === 'dark' ? 'light' : 'dark';
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, currentTheme);
    }
    applyTheme(currentTheme);
  },
  set(theme: Theme) {
    currentTheme = theme;
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, theme);
    }
    applyTheme(currentTheme);
  },
};
