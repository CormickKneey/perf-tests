import { useEffect, useRef, useState } from "react";
import type { StackGroup } from "./aggregate";
import { fmtDate, STATES } from "./data";
import { useTween } from "./motion";
import { usePrefs } from "./prefs";

export type State = (typeof STATES)[number];

/** One accent for the cold path, then three steps of grey; values per theme live in styles.css. */
export const STATE_COLOR: Record<State, string> = {
  cold: "var(--s-cold)",
  "seed-warm": "var(--s-seed)",
  "peer-warm": "var(--s-peer)",
  "local-hit": "var(--s-local)",
};
const STATE_DASH: Record<State, string | undefined> = {
  cold: undefined,
  "seed-warm": undefined,
  "peer-warm": undefined,
  "local-hit": "3 3",
};

export interface Point {
  cat: number;
  state: State;
  value: number | null;
  n: number;
  runs: number;
}

/** Tracks the rendered width so SVG text stays at its CSS size. */
function useWidth(fallback: number) {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(fallback);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.max(280, Math.round(e.contentRect.width))));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w] as const;
}

const NICE = [1, 2, 5];

/** Log domain snapped outward to 1-2-5 steps. */
export function logDomain(values: number[]): [number, number] {
  const vs = values.filter((v) => v > 0);
  if (!vs.length) return [1, 10];
  let lo = Math.min(...vs);
  let hi = Math.max(...vs);
  if (hi / lo < 4) {
    const mid = Math.sqrt(lo * hi);
    lo = Math.min(lo, mid / 2);
    hi = Math.max(hi, mid * 2);
  }
  const down = (v: number) => {
    const p = 10 ** Math.floor(Math.log10(v));
    return [...NICE].reverse().map((m) => m * p).find((c) => c <= v * (1 + 1e-9)) ?? p;
  };
  const up = (v: number) => {
    const p = 10 ** Math.floor(Math.log10(v));
    return [...NICE, 10].map((m) => m * p).find((c) => c >= v * (1 - 1e-9)) ?? 10 * p;
  };
  return [down(lo / 1.25), up(hi * 1.25)];
}

function logTicks([lo, hi]: [number, number]): number[] {
  const all: number[] = [];
  for (let p = 10 ** Math.floor(Math.log10(lo)); p <= hi; p *= 10) for (const m of NICE) if (m * p >= lo * 0.999 && m * p <= hi * 1.001) all.push(m * p);
  const decades = all.filter((t) => Math.abs(Math.log10(t) - Math.round(Math.log10(t))) < 1e-9);
  return decades.length >= 3 ? decades : all;
}

const shortDate = (s: string) => fmtDate(s).slice(5, 10);

const H = 340;
const TIP_W = 296;

export function LineChart({
  groups,
  points,
  domain,
  fmt,
  tickFmt,
  unit,
}: {
  groups: StackGroup[];
  points: Point[];
  domain: [number, number];
  fmt: (v: number) => string;
  tickFmt: (v: number) => string;
  unit: string;
}) {
  const { t: tr } = usePrefs();
  const [ref, W] = useWidth(960);
  const [hover, setHover] = useState<number | null>(null);
  const [lastHover, setLastHover] = useState(0);
  const narrow = W < 560;
  const endLabelW = Math.max(...STATES.map((s) => [...tr.states[s]].reduce((w, ch) => w + (ch.charCodeAt(0) > 0x2e80 ? 11 : 6.6), 0)));
  const M = { top: 22, right: narrow ? 16 : Math.max(96, Math.ceil(endLabelW + 30)), bottom: 40, left: 52 };
  const iw = W - M.left - M.right;
  const ih = H - M.top - M.bottom;

  const visible = points.filter((p): p is Point & { value: number } => p.value != null && p.value > 0);

  // Everything that moves on a filter change is tweened in log / index space.
  const target: Record<string, number> = { lo: Math.log10(domain[0]), hi: Math.log10(domain[1]), nc: Math.max(1, groups.length) };
  groups.forEach((g, i) => (target[`x:${g.key}`] = i));
  for (const p of visible) target[`v:${groups[p.cat].key}|${p.state}`] = Math.log10(p.value);
  const t = useTween(target);
  const at = (k: string) => t[k] ?? target[k];

  const band = iw / at("nc");
  const xOf = (cat: number) => M.left + band * (at(`x:${groups[cat].key}`) + 0.5);
  const yOfLog = (lv: number) => M.top + ih * (1 - (lv - at("lo")) / (at("hi") - at("lo") || 1));
  const yOf = (p: Point) => yOfLog(at(`v:${groups[p.cat].key}|${p.state}`));

  const lines = STATES.map((state) => ({
    state,
    pts: visible
      .filter((p) => p.state === state)
      .sort((a, b) => a.cat - b.cat)
      .map((p) => ({ x: xOf(p.cat), y: yOf(p) })),
  }));

  const stateLabels = STATES.map((s) => tr.states[s]);
  const labels = lines
    .filter((l) => l.pts.length)
    .map((l) => ({ state: l.state, y: l.pts[l.pts.length - 1].y, x: l.pts[l.pts.length - 1].x }))
    .sort((a, b) => a.y - b.y);
  for (let i = 1; i < labels.length; i++) labels[i].y = Math.max(labels[i].y, labels[i - 1].y + 14);
  // Render in a fixed order: re-ordering DOM nodes would restart their entrance animation.
  labels.sort((a, b) => STATES.indexOf(a.state) - STATES.indexOf(b.state));

  const ticks = logTicks(domain);
  const labelW = Math.max(...groups.map((g) => g.label.length), 1) * 6.6 + 16;
  const every = Math.max(1, Math.ceil(labelW / Math.max(1, iw / groups.length)));

  const shown = hover ?? lastHover;
  const g = groups[shown] ?? null;
  const cx = g ? M.left + (iw / groups.length) * (shown + 0.5) : 0;
  const rows = points.filter((p) => p.cat === shown && p.value != null);
  const tipLeft = Math.max(0, Math.min(W - TIP_W, cx < W / 2 ? cx + 20 : cx - 20 - TIP_W));

  const enter = (i: number) => {
    setHover(i);
    setLastHover(i);
  };

  return (
    <div className="plot" ref={ref} onMouseLeave={() => setHover(null)}>
      <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} role="img" aria-label={tr.chartLabel(unit)}>
        <text className="unit" x={M.left - 10} y={M.top - 10} textAnchor="end">
          {unit}
        </text>
        {ticks.map((v) => {
          const y = yOfLog(Math.log10(v));
          return (
            <g key={v} className="tick-row">
              <line className="grid" x1={M.left} x2={M.left + iw} y1={y} y2={y} />
              <text className="tick" x={M.left - 10} y={y} dy="0.32em" textAnchor="end">
                {tickFmt(v)}
              </text>
            </g>
          );
        })}
        <line className="axis" x1={M.left} x2={M.left + iw} y1={M.top + ih} y2={M.top + ih} />
        {groups.map((c, i) =>
          i % every === 0 || i === groups.length - 1 ? (
            <g key={c.key} className={hover === i ? "cat on" : "cat"}>
              <line className="xtick" x1={xOf(i)} x2={xOf(i)} y1={M.top + ih} y2={M.top + ih + 4} />
              <text className="xl" x={xOf(i)} y={M.top + ih + 22} textAnchor="middle">
                {c.label}
              </text>
            </g>
          ) : null,
        )}
        <line className={`crosshair${hover != null ? " on" : ""}`} x1={0} x2={0} y1={M.top} y2={M.top + ih} style={{ transform: `translateX(${cx}px)` }} />
        {lines.map((l, si) =>
          l.pts.length > 1 ? (
            <polyline
              key={l.state}
              className="line"
              points={l.pts.map((p) => `${p.x},${p.y}`).join(" ")}
              style={{ animationDelay: `${si * 70}ms`, stroke: STATE_COLOR[l.state] }}
              strokeDasharray={STATE_DASH[l.state]}
            />
          ) : null,
        )}
        {visible.map((p) => {
          const x = xOf(p.cat);
          const y = yOf(p);
          const on = hover === p.cat;
          return (
            <g key={`${groups[p.cat].key}|${p.state}`} className="pt" style={{ animationDelay: `${200 + STATES.indexOf(p.state) * 70}ms` }}>
              <rect
                className={on ? "mk on" : "mk"}
                x={x - 3}
                y={y - 3}
                width={6}
                height={6}
                style={{ fill: on ? STATE_COLOR[p.state] : "var(--bg)", stroke: STATE_COLOR[p.state] }}
                strokeWidth={1.25}
              />
            </g>
          );
        })}
        {!narrow &&
          labels.map((l) => (
            <g key={l.state} className="dl" style={{ transform: `translate(${M.left + iw + 14}px, ${l.y}px)` }}>
              <rect x={0} y={-3} width={6} height={6} style={{ fill: STATE_COLOR[l.state] }} />
              <text x={12} y={0} dy="0.32em">
                {tr.states[l.state]}
              </text>
            </g>
          ))}
        {groups.map((c, i) => (
          <rect
            key={c.key}
            className="hit"
            x={M.left + (iw / groups.length) * i}
            y={0}
            width={iw / groups.length}
            height={H}
            tabIndex={0}
            aria-label={c.label}
            onMouseEnter={() => enter(i)}
            onFocus={() => enter(i)}
            onBlur={() => setHover(null)}
            onClick={() => enter(i)}
          />
        ))}
      </svg>
      {narrow && (
        <div className="legend" aria-hidden="true">
          {STATES.map((s, i) => (
            <span key={s}>
              <i style={{ background: STATE_COLOR[s] }} />
              {stateLabels[i]}
            </span>
          ))}
        </div>
      )}
      {g && (
        <div className={`tip${hover != null ? " on" : ""}`} role="status" aria-hidden={hover == null} style={{ transform: `translate3d(${tipLeft}px, ${M.top}px, 0)` }}>
          <div className="tip-head">{g.label}</div>
          <dl className="tip-stack">
            {g.components.map((c) => (
              <div key={c.component}>
                <dt>{c.component}</dt>
                <dd>
                  {c.tag}
                  {c.version && <span className="muted"> {c.version}</span>}
                </dd>
              </div>
            ))}
          </dl>
          <table>
            <tbody>
              {rows.map((p) => (
                <tr key={p.state}>
                  <td>
                    <span className="key">
                      <i style={{ background: STATE_COLOR[p.state] }} />
                      {tr.states[p.state]}
                    </span>
                  </td>
                  <td className="v">{fmt(p.value!)}</td>
                  <td className="muted">n={p.n}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <ul className="tip-meta">
            {g.platforms.map((p) => (
              <li key={p.key}>
                {p.key}
                <span className="muted">
                  {" "}
                  · {tr.runs(p.runs)}
                  {p.nonBaseline ? ` · ${tr.nonBaseline}` : ""}
                </span>
              </li>
            ))}
            <li className="muted">
              {tr.runs(g.runs)} · {shortDate(g.first)}
              {shortDate(g.first) !== shortDate(g.latest) ? ` – ${shortDate(g.latest)}` : ""}
            </li>
          </ul>
        </div>
      )}
    </div>
  );
}
