import { useEffect, useState } from "react";

export type DesktopTheme = "light" | "dark";

const desktopThemeStorageKey = "speechkit.desktop.theme";

function readStoredTheme(fallback: DesktopTheme): DesktopTheme {
  if (typeof window === "undefined") {
    return fallback;
  }
  const stored = window.localStorage.getItem(desktopThemeStorageKey);
  return stored === "light" || stored === "dark" ? stored : fallback;
}

function applyTheme(theme: DesktopTheme) {
  if (typeof document === "undefined") {
    return;
  }
  document.documentElement.dataset.theme = theme;
  document.documentElement.classList.toggle("dark", theme === "dark");
}

export function useDesktopTheme(
  fallback: DesktopTheme = "dark",
): { theme: DesktopTheme; toggleTheme: () => void } {
  const [theme, setTheme] = useState<DesktopTheme>(() => readStoredTheme(fallback));

  useEffect(() => {
    applyTheme(theme);
    if (typeof window !== "undefined") {
      window.localStorage.setItem(desktopThemeStorageKey, theme);
    }
  }, [theme]);

  return {
    theme,
    toggleTheme: () =>
      setTheme((current) => (current === "dark" ? "light" : "dark")),
  };
}
