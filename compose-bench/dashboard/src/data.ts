import { useEffect, useState } from "react";

// Shapes follow schemas/*.schema.json in the repository.
export interface IndexEntry {
  id: string;
  mode: "smoke" | "compare";
  profile: string;
  verdict: string;
  exit_code: number;
  started_at: string;
  finished_at: string;
  non_baseline: boolean;
  classification: string;
  baseline: string;
  candidate?: string;
  overlays?: string[] | null;
  blocks: number;
  reasons?: string[] | null;
  has_report: boolean;
}

export interface ResultsIndex {
  schema: string;
  generated_at: string | null;
  runs: IndexEntry[];
}

export interface Summary {
  n: number;
  min: number;
  p10: number;
  median: number;
  mean: number;
  p90: number;
  max: number;
  stddev: number;
  cv: number;
}

export interface Group {
  state: string;
  file: string;
  size_bytes: number;
  baseline: Summary;
  candidate?: Summary;
  baseline_mib_per_s: Summary;
  candidate_mib_per_s?: Summary;
  delta_pct: number | null;
  delta_ms: number | null;
  p: number | null;
  p_adj: number | null;
  method?: string;
  ratio_ci_lo: number | null;
  ratio_ci_hi: number | null;
  verdict: string;
  reason: string;
  baseline_durations_ms: number[] | null;
  candidate_durations_ms?: number[] | null;
}

export interface Check {
  name: string;
  ok: boolean;
  required: boolean;
  detail: string;
}

export interface BlockSummary {
  id: string;
  variant: string;
  index: number;
  order: number;
  status: string;
  reasons?: string[] | null;
  samples: number;
  failures: number;
  checks: Check[] | null;
}

export interface VariantStats {
  blocks: number;
  valid_blocks: number;
  measured: number;
  failures: number;
  failure_rate: number;
}

export interface Report {
  run: string;
  mode: string;
  verdict: string;
  exit_code: number;
  reasons: string[] | null;
  non_baseline: boolean;
  policy: {
    alpha: number;
    regression_threshold: number;
    min_abs_delta_ms: number;
    min_samples: number;
    min_blocks: number;
    max_failure_rate: number;
  };
  variants: Record<string, VariantStats>;
  groups: Group[] | null;
  blocks: BlockSummary[] | null;
  generated_at: string;
}

export interface Image {
  component: string;
  ref: string;
  pinned: string;
  image_id: string;
  registry_digest?: string;
  platform: string;
  version?: string;
  source: string;
}

export interface Variant {
  name: string;
  scheduler: Image;
  client: Image;
  driver: Image;
  overlays: string[] | null;
}

export interface Run {
  id: string;
  mode: string;
  profile: string;
  params: Record<string, string>;
  baseline: Variant;
  candidate?: Variant;
  support_images: Image[] | null;
  host: {
    hostname: string;
    os: string;
    kernel: string;
    arch: string;
    cpus: number;
    memory_bytes: number;
    docker_version: string;
    compose_version: string;
    docker_os: string;
    docker_kernel: string;
    docker_cpus: number;
    docker_memory_bytes: number;
    classification: string;
    baseline_eligible: boolean;
    note: string;
  };
  non_baseline: boolean;
  block_order: string[] | null;
  fixtures: { volume: string; spec_hash: string; seed: string; files: { name: string; size: number; sha256: string }[] | null };
  git_commit: string;
  git_dirty: boolean;
  started_at: string;
  finished_at: string;
  exit_code: number;
  verdict: string;
}

export type Load<T> = { state: "loading" } | { state: "error"; error: string } | { state: "ready"; data: T };

const cache = new Map<string, Promise<unknown>>();

/** Public Supabase URL + anon/publishable key. Not a user login. */
export function supabaseBrowserConfig(): { url: string; key: string } | null {
  const url = (import.meta.env.VITE_SUPABASE_URL ?? "").trim().replace(/\/$/, "");
  const key = (import.meta.env.VITE_SUPABASE_ANON_KEY ?? import.meta.env.VITE_SUPABASE_PUBLISHABLE_KEY ?? "").trim();
  if (url && key) return { url, key };
  return null;
}

export function configuredSource(): "supabase" | "local" {
  return supabaseBrowserConfig() ? "supabase" : "local";
}

async function cached<T>(key: string, loader: () => Promise<T>): Promise<T> {
  if (!cache.has(key)) {
    cache.set(key, loader());
  }
  try {
    return (await cache.get(key)) as T;
  } catch (e) {
    cache.delete(key);
    throw e;
  }
}

async function fetchLocalJSON<T>(path: string): Promise<T> {
  const r = await fetch(`./data/${path}`, { cache: "no-cache" });
  if (!r.ok) throw new Error(`${path}: HTTP ${r.status}`);
  return r.json() as Promise<T>;
}

async function supabaseFetch(pathAndQuery: string, init?: RequestInit): Promise<Response> {
  const cfg = supabaseBrowserConfig();
  if (!cfg) throw new Error("supabase is not configured");
  const r = await fetch(`${cfg.url}${pathAndQuery}`, {
    cache: "no-cache",
    ...init,
    headers: {
      apikey: cfg.key,
      Authorization: `Bearer ${cfg.key}`,
      Accept: "application/json",
      ...(init?.headers ?? {}),
    },
  });
  if (!r.ok) throw new Error(`supabase ${pathAndQuery}: HTTP ${r.status}`);
  return r;
}

export async function listRuns(): Promise<ResultsIndex> {
  return cached("index.json", async () => {
    const cfg = supabaseBrowserConfig();
    if (cfg) {
      const r = await supabaseFetch(
        "/rest/v1/runs?select=id,mode,profile,verdict,exit_code,started_at,finished_at,non_baseline,classification,baseline,candidate,overlays,blocks,reasons,has_report&published=eq.true&order=id.desc",
      );
      const rows = (await r.json()) as IndexEntry[];
      return { schema: "dfperf/v1", generated_at: new Date().toISOString(), runs: rows };
    }
    return fetchLocalJSON<ResultsIndex>("index.json");
  });
}

async function loadStoredJson<T>(id: string, column: "run_json" | "report_json" | "preflight_json"): Promise<T> {
  const r = await supabaseFetch(
    `/rest/v1/runs?select=${column}&id=eq.${encodeURIComponent(id)}&published=eq.true`,
  );
  const rows = (await r.json()) as Array<Record<string, T | null>>;
  const value = rows[0]?.[column];
  if (value == null) throw new Error(`${column} for run ${id} not found`);
  return value;
}

export async function getRun(id: string): Promise<Run> {
  return cached(`runs/${id}/run.json`, async () => {
    if (supabaseBrowserConfig()) return loadStoredJson<Run>(id, "run_json");
    return fetchLocalJSON<Run>(`runs/${id}/run.json`);
  });
}

export async function getReport(id: string): Promise<Report> {
  return cached(`runs/${id}/report.json`, async () => {
    if (supabaseBrowserConfig()) return loadStoredJson<Report>(id, "report_json");
    return fetchLocalJSON<Report>(`runs/${id}/report.json`);
  });
}

export async function getPreflight(id: string): Promise<unknown> {
  return cached(`runs/${id}/preflight.json`, async () => {
    if (supabaseBrowserConfig()) return loadStoredJson(id, "preflight_json");
    return fetchLocalJSON(`runs/${id}/preflight.json`);
  });
}

async function loadByPath<T>(path: string): Promise<T> {
  if (path === "index.json") return listRuns() as Promise<T>;
  const m = /^runs\/([0-9a-z]{8,})\/(run|report|preflight)\.json$/.exec(path);
  if (m) {
    const id = m[1];
    if (m[2] === "run") return getRun(id) as Promise<T>;
    if (m[2] === "report") return getReport(id) as Promise<T>;
    if (m[2] === "preflight") return getPreflight(id) as Promise<T>;
  }
  return fetchLocalJSON<T>(path);
}

export function useJSON<T>(path: string | null): Load<T> {
  const [st, setSt] = useState<Load<T>>({ state: "loading" });
  useEffect(() => {
    if (!path) return;
    let live = true;
    setSt({ state: "loading" });
    loadByPath<T>(path).then(
      (data) => live && setSt({ state: "ready", data }),
      (e: Error) => live && setSt({ state: "error", error: e.message }),
    );
    return () => {
      live = false;
    };
  }, [path]);
  return st;
}

export async function getText(path: string): Promise<string> {
  const md = /^runs\/([0-9a-z]{8,})\/report\.md$/.exec(path);
  const cfg = supabaseBrowserConfig();
  if (cfg && md) {
    const id = md[1];
    const r = await fetch(`${cfg.url}/storage/v1/object/dfperf-artifacts/runs/${encodeURIComponent(id)}/report.md`, {
      cache: "no-cache",
      headers: { apikey: cfg.key, Authorization: `Bearer ${cfg.key}` },
    });
    if (!r.ok) throw new Error(`${path}: HTTP ${r.status}`);
    return r.text();
  }
  const r = await fetch(`./data/${path}`, { cache: "no-cache" });
  if (!r.ok) throw new Error(`${path}: HTTP ${r.status}`);
  return r.text();
}

// Formatting helpers. Numbers are kept on one line by the callers (nowrap).
export const fmtMs = (v: number | null | undefined) =>
  v == null || Number.isNaN(v) ? "–" : v >= 1000 ? `${(v / 1000).toFixed(2)}\u00a0s` : v >= 100 ? `${v.toFixed(0)}\u00a0ms` : `${v.toFixed(1)}\u00a0ms`;
export const fmtPct = (v: number | null | undefined) => (v == null ? "–" : `${v > 0 ? "+" : v < 0 ? "−" : "±"}${Math.abs(v).toFixed(1)}%`);
export const fmtP = (v: number | null | undefined) => (v == null ? "–" : v < 0.001 ? v.toExponential(1) : v.toFixed(3));
export const fmtBytes = (n: number) =>
  n >= 1 << 30 ? `${n / (1 << 30)}\u00a0GiB` : n >= 1 << 20 ? `${n / (1 << 20)}\u00a0MiB` : n >= 1024 ? `${n / 1024}\u00a0KiB` : `${n}\u00a0B`;
export const fmtDate = (s: string) => {
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return "–";
  return d.toISOString().replace("T", " ").slice(0, 16) + " UTC";
};
export const shortDigest = (s?: string) => {
  if (!s) return "–";
  const i = s.indexOf("sha256:");
  return i >= 0 ? s.slice(0, i) + "sha256:" + s.slice(i + 7, i + 19) : s;
};

export const STATES = ["cold", "seed-warm", "peer-warm", "local-hit"] as const;

export const EXIT_MEANING: Record<number, string> = {
  0: "pass",
  1: "regression",
  2: "benchmark failed",
  3: "infra / invalid",
  4: "inconclusive",
};
