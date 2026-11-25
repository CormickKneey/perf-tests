import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type Theme = "light" | "dark";
export type Lang = "en" | "zh";

/** localStorage keys; index.html reads the same keys before first paint. */
export const THEME_KEY = "compose-bench.theme";
export const LANG_KEY = "compose-bench.lang";

const read = (k: string) => {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
};
const write = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v);
  } catch {
    /* private mode: keep the in-memory choice */
  }
};

/** Light unless the user saved a choice; prefers-color-scheme is deliberately ignored. */
export const initialTheme = (): Theme => (read(THEME_KEY) === "dark" ? "dark" : "light");

/** Saved choice, else zh* browser languages get Chinese, everything else English. */
export function initialLang(): Lang {
  const saved = read(LANG_KEY);
  if (saved === "zh" || saved === "en") return saved;
  const nav = typeof navigator === "undefined" ? [] : navigator.languages?.length ? navigator.languages : [navigator.language];
  return /^zh(-|_|$)/i.test(nav[0] ?? "") ? "zh" : "en";
}

const THEME_COLOR: Record<Theme, string> = { light: "#ffffff", dark: "#000000" };

export function applyTheme(theme: Theme) {
  const el = document.documentElement;
  el.dataset.theme = theme;
  el.style.colorScheme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", THEME_COLOR[theme]);
}

const en = {
  links: "Links",
  stat: "Stat",
  size: "Size",
  metric: "Metric",
  platform: "Platform",
  profile: "Profile",
  all: "all",
  duration: "duration",
  stats: { avg: "avg", p99: "p99", min: "min", max: "max" } as Record<string, string>,
  states: { cold: "cold", "seed-warm": "seed-warm", "peer-warm": "peer-warm", "local-hit": "local-hit" } as Record<string, string>,
  verdicts: {} as Record<string, string>,
  stack: "Stack",
  runsCol: "Runs",
  lastSeen: "Last seen",
  loading: "Loading",
  loadFailed: "Failed to load results",
  empty: "No runs with samples.",
  emptyFiltered: "No runs with samples for this filter.",
  runsOf: (label: string) => `Runs of ${label}`,
  runs: (n: number) => `${n} ${n === 1 ? "run" : "runs"}`,
  nonBaseline: "non-baseline",
  unit: { ms: "ms", mibs: "MiB/s" },
  chartLabel: (unit: string) => `${unit} by stack version, log scale`,
  legend: "Cache states",
  theme: "Theme",
  light: "Light theme",
  dark: "Dark theme",
  language: "Language",
};

export type Dict = typeof en;

const zh: Dict = {
  links: "链接",
  stat: "统计",
  size: "大小",
  metric: "指标",
  platform: "平台",
  profile: "场景",
  all: "全部",
  duration: "耗时",
  stats: { avg: "平均", p99: "p99", min: "最小", max: "最大" },
  states: { cold: "冷启动", "seed-warm": "种子预热", "peer-warm": "节点预热", "local-hit": "本地命中" },
  verdicts: { PASS: "通过", FAIL: "失败", REGRESSION: "回归", INCONCLUSIVE: "不确定", INVALID: "无效" },
  stack: "组件版本",
  runsCol: "次数",
  lastSeen: "最近",
  loading: "加载中",
  loadFailed: "结果加载失败",
  empty: "没有包含样本的运行。",
  emptyFiltered: "当前筛选下没有包含样本的运行。",
  runsOf: (label: string) => `${label} 的运行记录`,
  runs: (n: number) => `${n} 次运行`,
  nonBaseline: "非基准环境",
  unit: { ms: "ms", mibs: "MiB/s" },
  chartLabel: (unit: string) => `各组件版本的${unit === "ms" ? "耗时" : "吞吐"}（${unit}），对数坐标`,
  legend: "缓存状态",
  theme: "主题",
  light: "浅色主题",
  dark: "深色主题",
  language: "语言",
};

export const DICTS: Record<Lang, Dict> = { en, zh };

interface Prefs {
  theme: Theme;
  setTheme: (t: Theme) => void;
  lang: Lang;
  setLang: (l: Lang) => void;
  t: Dict;
}

const Ctx = createContext<Prefs | null>(null);

export function PrefsProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(initialTheme);
  const [lang, setLangState] = useState<Lang>(initialLang);

  useEffect(() => applyTheme(theme), [theme]);
  useEffect(() => {
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
  }, [lang]);

  const setTheme = useCallback((t: Theme) => {
    write(THEME_KEY, t);
    setThemeState(t);
  }, []);
  const setLang = useCallback((l: Lang) => {
    write(LANG_KEY, l);
    setLangState(l);
  }, []);

  const value = useMemo(() => ({ theme, setTheme, lang, setLang, t: DICTS[lang] }), [theme, setTheme, lang, setLang]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function usePrefs(): Prefs {
  const p = useContext(Ctx);
  if (!p) throw new Error("usePrefs outside PrefsProvider");
  return p;
}
