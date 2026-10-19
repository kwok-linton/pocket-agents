// Interface language. Source strings are the Chinese text itself: t("正在读取…")
// returns it unchanged in Chinese and looks it up in locales/en.ts in English.
// Server text that reaches the screen goes through tServer, which also maps
// the backend's English messages to Chinese. The language is read once at
// startup; changing it reloads the page so module-level labels follow.
// Only the active language's table is downloaded.

export type Language = "zh" | "en";
export type LanguageSetting = Language | "system";
export type Table = Record<string, string>;
const STORAGE_KEY = "pocket:language";

export function languageSetting(): LanguageSetting {
  try {
    const value = globalThis.localStorage?.getItem(STORAGE_KEY);
    if (value === "zh" || value === "en" || value === "system") return value;
  } catch {
    /* Storage can be unavailable in private mode. */
  }
  return "zh";
}
export function resolveLanguage(setting: LanguageSetting, preferred: readonly string[] = []): Language {
  if (setting !== "system") return setting;
  return preferred.find(Boolean)?.toLowerCase().startsWith("zh") ? "zh" : "en";
}
const browser = typeof window !== "undefined" && typeof document !== "undefined";
export const language: Language = resolveLanguage(
  languageSetting(),
  browser ? navigator.languages || [navigator.language] : [],
);
if (browser) {
  document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
  // The service worker cannot read localStorage; push notifications read the
  // language from here (see public/sw.js).
  globalThis.caches?.open("pocket-preferences-v1")
    .then((cache) => cache.put("/.pocket-language", new Response(language)))
    .catch(() => {});
}

// Interface text needs a table only in English; server text needs one either way.
const interfaceTable: Table = language === "en" ? (await import("./locales/en.ts")).en : {};
const serverTable: Table = language === "en" ? interfaceTable : (await import("./locales/zh-server.ts")).zhFromServer;

export function setLanguage(setting: LanguageSetting) {
  try {
    localStorage.setItem(STORAGE_KEY, setting);
  } catch {
    /* Without storage the choice cannot persist; keep the current page. */
    return;
  }
  location.reload();
}

/** BCP 47 tag for Intl formatters in the current language. */
export const locale =
  language === "zh" ? "zh-CN" : browser && navigator.language?.startsWith("en") ? navigator.language : "en-US";

type Params = Record<string, string | number | boolean | null | undefined>;
// {0} inserts a value; {0|session|sessions} picks a form by the number, so
// English can say "1 session" / "3 sessions" where Chinese needs no plural.
function fill(text: string, params?: Params) {
  if (!params) return text;
  return text
    .replace(/\{(\w+)\|([^|{}]*)\|([^{}]*)\}/g, (match, key, one, many) => (key in params ? (Number(params[key]) === 1 ? one : many) : match))
    // Like JSX, false/null/undefined render as nothing.
    .replace(/\{(\w+)\}/g, (match, key) => (key in params ? (params[key] === false ? "" : String(params[key] ?? "")) : match));
}
export function translate(text: string, table: Table, params?: Params) {
  return fill(table[text] ?? text, params);
}
export function t(text: string, params?: Params) {
  return translate(text, interfaceTable, params);
}

// Backend messages are mostly fixed sentences, some followed by a detail
// ("...: <error>"). Entries ending in ": " or "：" match as prefixes.
export function translateServer(text: string | undefined, table: Table) {
  if (!text) return text || "";
  if (text in table) return table[text];
  for (const key in table)
    if ((key.endsWith(": ") || key.endsWith("：")) && text.startsWith(key)) return table[key] + text.slice(key.length);
  return text;
}
export function tServer(text: string | undefined) {
  return translateServer(text, serverTable);
}
