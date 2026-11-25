// Pure aggregation helpers for the dashboard. No DOM, no fetch: the node test
// in scripts/test-aggregate.mjs imports this file directly.
import type { Group, Image, Report, Run, Variant } from "./data";

export const STATS = ["avg", "max", "min", "p99"] as const;
export type Stat = (typeof STATS)[number];
export type Metric = "ms" | "mibs";
export type Side = "baseline" | "candidate";

/** Linear-interpolated percentile on the sorted samples; q in [0, 1]. */
export function percentile(samples: readonly number[], q: number): number | null {
  const n = samples.length;
  if (n === 0) return null;
  const x = [...samples].sort((a, b) => a - b);
  if (n === 1) return x[0];
  const index = q * (n - 1);
  const lo = Math.floor(index);
  const hi = Math.ceil(index);
  return x[lo] + (index - lo) * (x[hi] - x[lo]);
}

export function applyStat(samples: readonly number[], stat: Stat): number | null {
  if (samples.length === 0) return null;
  switch (stat) {
    case "avg":
      return samples.reduce((s, v) => s + v, 0) / samples.length;
    case "max":
      return Math.max(...samples);
    case "min":
      return Math.min(...samples);
    case "p99":
      return percentile(samples, 0.99);
  }
}

/** Per-sample throughput in MiB/s from a duration in ms. */
export const mibPerS = (sizeBytes: number, ms: number) => sizeBytes / 1048576 / (ms / 1000);

export interface Member {
  run: Run;
  report: Report;
}

/** One side of one run: a compare run yields a baseline and a candidate contribution. */
export interface Contribution {
  run: Run;
  report: Report;
  side: Side;
  variant: Variant;
}

// ---------------------------------------------------------------------------
// Stack version

const ORDER = ["scheduler", "client", "manager", "seed-client"];
const isImage = (v: unknown): v is Image => !!v && typeof v === "object" && typeof (v as Image).component === "string" && typeof (v as Image).pinned === "string";

/** Pinned Dragonfly components of a variant. The driver is derived from the client and is not a component. */
export function stackImages(v: Variant): Image[] {
  return Object.values(v)
    .filter(isImage)
    .filter((i) => i.component !== "driver" && i.source !== "derived")
    .sort((a, b) => {
      const ia = ORDER.indexOf(a.component);
      const ib = ORDER.indexOf(b.component);
      return (ia < 0 ? ORDER.length : ia) - (ib < 0 ? ORDER.length : ib) || a.component.localeCompare(b.component);
    });
}

const digestOf = (s?: string) => /sha256:[0-9a-f]{12,}/.exec(s ?? "")?.[0];

/**
 * Platform-independent identity of one pinned component. The per-arch image
 * digest differs between amd64 and arm64 pulls of the same release, so the
 * self-reported build version (which carries the commit) comes first, then the
 * registry (index) digest, then the pinned digest.
 */
export function componentIdent(i: Image): string {
  return i.version?.trim() || i.registry_digest || digestOf(i.pinned) || i.pinned;
}

/** Stack-version key: component=identity for every pinned component. No mode, host, platform or side. */
export function stackKey(v: Variant): string {
  return stackImages(v)
    .map((i) => `${i.component}=${componentIdent(i)}`)
    .join("|");
}

/** "dragonflyoss/scheduler:v2.5.4" -> "v2.5.4"; digest refs -> "@1a2b3c4". */
export function shortRef(ref: string): string {
  const d = /@sha256:([0-9a-f]{7})/.exec(ref);
  if (d) return `@${d[1]}`;
  const i = ref.lastIndexOf(":");
  return i > ref.lastIndexOf("/") ? ref.slice(i + 1) : ref;
}

// ---------------------------------------------------------------------------
// Platform (metadata only, never part of the stack key)

export interface Platform {
  key: string;
  classification: string;
  os: string;
  arch: string;
  nonBaseline: boolean;
}

export const platformOf = (run: Run): Platform => ({
  key: `${run.host.classification} ${run.host.os}/${run.host.arch}`,
  classification: run.host.classification,
  os: run.host.os,
  arch: run.host.arch,
  nonBaseline: run.non_baseline,
});

// ---------------------------------------------------------------------------
// Grouping

function sideDurations(g: Group, side: Side): number[] {
  return (side === "baseline" ? g.baseline_durations_ms : g.candidate_durations_ms) ?? [];
}

const sideHasSamples = (r: Report, side: Side) => (r.groups ?? []).some((g) => sideDurations(g, side).length > 0);

export function hasSamples(report: Report | null | undefined): boolean {
  return !!report && report.verdict !== "INVALID" && sideHasSamples(report, "baseline");
}

/** Valid runs split into per-side contributions. */
export function contributions(members: readonly Member[]): Contribution[] {
  const out: Contribution[] = [];
  for (const m of members) {
    if (m.run.verdict === "INVALID" || !hasSamples(m.report)) continue;
    out.push({ ...m, side: "baseline", variant: m.run.baseline });
    if (m.run.candidate && sideHasSamples(m.report, "candidate")) out.push({ ...m, side: "candidate", variant: m.run.candidate });
  }
  return out;
}

export interface StackComponent {
  component: string;
  ref: string;
  tag: string;
  version?: string;
  ident: string;
}

export interface StackGroup {
  key: string;
  /** short display label, e.g. "v2.5.4 · v1.5.14" */
  label: string;
  components: StackComponent[];
  first: string;
  latest: string;
  contribs: Contribution[];
  /** distinct run ids */
  runs: number;
  /** platforms that contributed, with distinct run counts */
  platforms: (Platform & { runs: number })[];
}

export interface GroupFilter {
  platform?: string | null;
  profile?: string | null;
}

export function matches(c: { run: Run }, f: GroupFilter): boolean {
  if (f.profile && c.run.profile !== f.profile) return false;
  if (f.platform && platformOf(c.run).key !== f.platform) return false;
  return true;
}

/** Pools every contribution of the same stack version, ordered by first seen. */
export function groupStacks(members: readonly Member[], filter: GroupFilter = {}): StackGroup[] {
  const byKey = new Map<string, StackGroup>();
  for (const c of contributions(members)) {
    if (!matches(c, filter)) continue;
    const key = stackKey(c.variant);
    let g = byKey.get(key);
    if (!g) {
      g = { key, label: "", components: [], first: c.run.started_at, latest: c.run.started_at, contribs: [], runs: 0, platforms: [] };
      byKey.set(key, g);
    }
    g.contribs.push(c);
    if (c.run.started_at < g.first) g.first = c.run.started_at;
    if (c.run.started_at >= g.latest) g.latest = c.run.started_at;
  }
  const out = [...byKey.values()].sort((a, b) => a.first.localeCompare(b.first) || a.key.localeCompare(b.key));
  for (const g of out) {
    g.contribs.sort((a, b) => a.run.started_at.localeCompare(b.run.started_at) || a.side.localeCompare(b.side));
    // Refs come from the most recent contribution; the identity is the same for all of them.
    const newest = g.contribs[g.contribs.length - 1].variant;
    g.components = stackImages(newest).map((i) => ({ component: i.component, ref: i.ref, tag: shortRef(i.ref), version: i.version, ident: componentIdent(i) }));
    g.label = g.components.map((c) => c.tag).join(" · ");
    const runIds = new Set<string>();
    const plats = new Map<string, Platform & { ids: Set<string> }>();
    for (const c of g.contribs) {
      runIds.add(c.run.id);
      const p = platformOf(c.run);
      if (!plats.has(p.key)) plats.set(p.key, { ...p, ids: new Set() });
      plats.get(p.key)!.ids.add(c.run.id);
    }
    g.runs = runIds.size;
    g.platforms = [...plats.values()]
      .map(({ ids, ...p }) => ({ ...p, runs: ids.size }))
      .sort((a, b) => Number(a.nonBaseline) - Number(b.nonBaseline) || a.key.localeCompare(b.key));
  }
  // Two stacks can share tags (e.g. a re-pushed "latest"); keep labels unique.
  const seen = new Map<string, number>();
  for (const g of out) {
    const n = (seen.get(g.label) ?? 0) + 1;
    seen.set(g.label, n);
    if (n > 1) g.label = `${g.label} #${n}`;
  }
  return out;
}

// ---------------------------------------------------------------------------
// Cells

/** Raw samples for one (state, file) cell, concatenated across contributions. */
export function cellSamples(contribs: readonly Contribution[], state: string, file: string, metric: Metric): number[] {
  const out: number[] = [];
  for (const c of contribs) {
    for (const g of c.report.groups ?? []) {
      if (g.state !== state || g.file !== file) continue;
      for (const ms of sideDurations(g, c.side)) out.push(metric === "ms" ? ms : mibPerS(g.size_bytes, ms));
    }
  }
  return out;
}

export interface Cell {
  value: number | null;
  n: number;
  runs: number;
}

export function cell(contribs: readonly Contribution[], state: string, file: string, metric: Metric, stat: Stat): Cell {
  const samples = cellSamples(contribs, state, file, metric);
  const ids = new Set<string>();
  for (const c of contribs) if ((c.report.groups ?? []).some((g) => g.state === state && g.file === file && sideDurations(g, c.side).length > 0)) ids.add(c.run.id);
  return { value: applyStat(samples, stat), n: samples.length, runs: ids.size };
}

// ---------------------------------------------------------------------------
// Filter options

/** Files present in any valid report, smallest first. */
export function filesOf(members: readonly Member[]): { file: string; size: number }[] {
  const seen = new Map<string, number>();
  for (const c of contributions(members)) for (const r of c.report.groups ?? []) seen.set(r.file, r.size_bytes);
  return [...seen.entries()].map(([file, size]) => ({ file, size })).sort((a, b) => a.size - b.size);
}

/** Platforms of valid runs; baseline-eligible hosts first. */
export function platformsOf(members: readonly Member[]): Platform[] {
  const seen = new Map<string, Platform>();
  for (const c of contributions(members)) {
    const p = platformOf(c.run);
    if (!seen.has(p.key)) seen.set(p.key, p);
  }
  return [...seen.values()].sort((a, b) => Number(a.nonBaseline) - Number(b.nonBaseline) || a.key.localeCompare(b.key));
}

/** Profiles of valid runs, most runs first. */
export function profilesOf(members: readonly Member[]): string[] {
  const n = new Map<string, Set<string>>();
  for (const c of contributions(members)) {
    if (!n.has(c.run.profile)) n.set(c.run.profile, new Set());
    n.get(c.run.profile)!.add(c.run.id);
  }
  return [...n.entries()].sort((a, b) => b[1].size - a[1].size || a[0].localeCompare(b[0])).map(([p]) => p);
}
