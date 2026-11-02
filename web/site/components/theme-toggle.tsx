"use client";

import { Moon, Sun } from "lucide-react";

export function ThemeToggle() {
  const toggle = () => {
    const dark = document.documentElement.classList.toggle("dark");
    try {
      localStorage.setItem("skiff-theme", dark ? "dark" : "light");
    } catch {
      /* the choice still applies until reload */
    }
  };
  return (
    <button
      type="button"
      onClick={toggle}
      aria-label="Switch between light and dark"
      title="Switch between light and dark"
      className="text-muted hover:text-fg border-line grid h-9 w-9 cursor-pointer place-items-center rounded-lg border transition-colors"
    >
      <Sun className="hidden h-4 w-4 dark:block" />
      <Moon className="h-4 w-4 dark:hidden" />
    </button>
  );
}
