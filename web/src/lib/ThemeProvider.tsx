import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import {
  isTheme,
  THEME_STORAGE_KEY,
  ThemeContext,
  type ResolvedTheme,
  type Theme,
  type ThemeContextValue,
} from "@/lib/theme";

const DARK_QUERY = "(prefers-color-scheme: dark)";

type Props = {
  children: ReactNode;
};

function readStoredTheme(): Theme {
  const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
  if (stored !== null && isTheme(stored)) return stored;
  return "system";
}

function subscribeSystemDark(onChange: () => void): () => void {
  const mq = window.matchMedia(DARK_QUERY);
  mq.addEventListener("change", onChange);
  return () => {
    mq.removeEventListener("change", onChange);
  };
}

function systemDarkSnapshot(): boolean {
  return window.matchMedia(DARK_QUERY).matches;
}

export function ThemeProvider({ children }: Props) {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme);
  const systemDark = useSyncExternalStore(
    subscribeSystemDark,
    systemDarkSnapshot,
  );

  let resolvedTheme: ResolvedTheme = theme === "dark" ? "dark" : "light";
  if (theme === "system") resolvedTheme = systemDark ? "dark" : "light";

  useEffect(() => {
    document.documentElement.classList.toggle("dark", resolvedTheme === "dark");
  }, [resolvedTheme]);

  const setTheme = useCallback((next: Theme) => {
    window.localStorage.setItem(THEME_STORAGE_KEY, next);
    setThemeState(next);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({ theme, resolvedTheme, setTheme }),
    [theme, resolvedTheme, setTheme],
  );

  return <ThemeContext value={value}>{children}</ThemeContext>;
}
