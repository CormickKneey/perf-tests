import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { cell, filesOf, groupStacks, platformsOf, profilesOf, STATS, type Member, type Metric, type Stat } from "./aggregate";
import { LineChart, logDomain, type Point } from "./Chart";
import { fmtBytes, fmtDate, fmtMs, getReport, getRun, listRuns, STATES } from "./data";
import { usePrefs, type Lang, type Theme } from "./prefs";
import { Verdict } from "./ui";

type Loaded = { state: "loading" } | { state: "error"; error: string } | { state: "ready"; members: Member[] };

function useResults(): Loaded {
  const [st, setSt] = useState<Loaded>({ state: "loading" });
  useEffect(() => {
    let live = true;
    (async () => {
      const index = await listRuns();
      const members = await Promise.all(
        (index.runs ?? [])
          .filter((r) => r.has_report && r.verdict !== "INVALID")
          .map(async (r) => {
            try {
              const [run, report] = await Promise.all([getRun(r.id), getReport(r.id)]);
              return { run, report } as Member;
            } catch {
              return null;
            }
          }),
      );
      if (live) setSt({ state: "ready", members: members.filter((m): m is Member => m != null) });
    })().catch((e: Error) => live && setSt({ state: "error", error: e.message }));
    return () => {
      live = false;
    };
  }, []);
  return st;
}

const fmtMiBs = (v: number) => `${v >= 100 ? v.toFixed(0) : v.toFixed(1)}\u00a0MiB/s`;
const fmtAxisMs = (v: number) => (v >= 1000 ? `${+(v / 1000).toFixed(1)}s` : `${+v.toFixed(1)}ms`);
const fmtAxisMiBs = (v: number) => (v >= 1000 ? `${+(v / 1000).toFixed(1)}k` : `${+v.toFixed(v >= 10 ? 0 : 1)}`);

function Select<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: { value: T; label: string }[]; onChange: (v: T) => void }) {
  return (
    <label className="filter">
      <span className="filter-label">{label}</span>
      <span className="filter-box">
        <select value={value} onChange={(e) => onChange(e.target.value as T)}>
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <svg viewBox="0 0 10 10" aria-hidden="true">
          <path d="M2 3.5 5 6.5 8 3.5" fill="none" stroke="currentColor" strokeWidth="1.2" />
        </svg>
      </span>
    </label>
  );
}

function Delta({ v, base, metric }: { v: number | null; base: number | null; metric: Metric }) {
  if (v == null || base == null || base === 0) return null;
  const pct = ((v - base) / base) * 100;
  if (Math.abs(pct) < 0.05) return null;
  const worse = metric === "ms" ? pct > 0 : pct < 0;
  return <span className={`delta${worse ? " worse" : ""}`}>{`${pct > 0 ? "+" : "−"}${Math.abs(pct).toFixed(1)}%`}</span>;
}

function Segmented<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: { value: T; label: ReactNode; title: string }[]; onChange: (v: T) => void }) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === value} aria-label={o.title} title={o.title} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

const SUN = (
  <svg viewBox="0 0 12 12" aria-hidden="true">
    <rect x="4" y="4" width="4" height="4" fill="currentColor" />
    <path d="M6 0.5v1.5M6 10v1.5M0.5 6H2M10 6h1.5M2.1 2.1l1 1M8.9 8.9l1 1M2.1 9.9l1-1M8.9 3.1l1-1" stroke="currentColor" strokeWidth="1" />
  </svg>
);
const MOON = (
  <svg viewBox="0 0 12 12" aria-hidden="true">
    <path d="M8.5 1.5A4.5 4.5 0 1 0 10.5 8 3.6 3.6 0 0 1 8.5 1.5Z" fill="none" stroke="currentColor" strokeWidth="1" />
  </svg>
);

function PrefControls() {
  const { t, theme, setTheme, lang, setLang } = usePrefs();
  return (
    <div className="prefs">
      <Segmented<Lang>
        label={t.language}
        value={lang}
        onChange={setLang}
        options={[
          { value: "en", label: "EN", title: "English" },
          { value: "zh", label: "中", title: "中文" },
        ]}
      />
      <Segmented<Theme>
        label={t.theme}
        value={theme}
        onChange={setTheme}
        options={[
          { value: "light", label: SUN, title: t.light },
          { value: "dark", label: MOON, title: t.dark },
        ]}
      />
    </div>
  );
}

const ALL = "";

export default function Dashboard() {
  const res = useResults();
  const { t } = usePrefs();
  const [stat, setStat] = useState<Stat>("avg");
  const [metric, setMetric] = useState<Metric>("ms");
  const [file, setFile] = useState<string | null>(null);
  const [platform, setPlatform] = useState<string>(ALL);
  const [profile, setProfile] = useState<string | null>(null);
  const [open, setOpen] = useState<string | null>(null);

  const members = res.state === "ready" ? res.members : null;
  const files = useMemo(() => (members ? filesOf(members) : []), [members]);
  const platforms = useMemo(() => (members ? platformsOf(members) : []), [members]);
  const profiles = useMemo(() => (members ? profilesOf(members) : []), [members]);
  const activeProfile = profile ?? profiles[0] ?? null;
  const groups = useMemo(() => (members ? groupStacks(members, { platform: platform || null, profile: activeProfile }) : []), [members, platform, activeProfile]);
  const activeFile = file ?? files[files.length - 1]?.file ?? null;
  const fmt = metric === "ms" ? fmtMs : fmtMiBs;

  const points = useMemo(
    () => (activeFile == null ? [] : groups.flatMap((g, cat) => STATES.map((state): Point => ({ cat, state, ...cell(g.contribs, state, activeFile, metric, stat) })))),
    [groups, activeFile, metric, stat],
  );
  const domain = useMemo(() => logDomain(points.map((p) => p.value ?? 0)), [points]);
  const value = (cat: number, si: number) => points[cat * STATES.length + si]?.value ?? null;

  return (
    <div className="container">
      <div className="filters reveal" style={{ ["--i" as string]: 0 }}>
        <Select label={t.stat} value={stat} onChange={setStat} options={STATS.map((s) => ({ value: s, label: t.stats[s] ?? s }))} />
        {activeFile && <Select label={t.size} value={activeFile} onChange={setFile} options={files.map((f) => ({ value: f.file, label: fmtBytes(f.size) }))} />}
        <Select
          label={t.metric}
          value={metric}
          onChange={setMetric}
          options={[
            { value: "ms", label: t.duration },
            { value: "mibs", label: "MiB/s" },
          ]}
        />
        {platforms.length > 0 && (
          <Select label={t.platform} value={platform} onChange={setPlatform} options={[{ value: ALL, label: t.all }, ...platforms.map((p) => ({ value: p.key, label: p.key }))]} />
        )}
        {profiles.length > 1 && activeProfile && <Select label={t.profile} value={activeProfile} onChange={setProfile} options={profiles.map((p) => ({ value: p, label: p }))} />}
        <PrefControls />
      </div>

      {res.state === "loading" && (
        <p className="status" role="status">
          <span className="pulse" aria-hidden="true" />
          {t.loading}
        </p>
      )}
      {res.state === "error" && (
        <p className="status err" role="alert">
          {t.loadFailed}
          {res.error ? <code className="err-detail">{res.error}</code> : null}
        </p>
      )}
      {res.state === "ready" && groups.length === 0 && <p className="status">{platform || profiles.length > 1 ? t.emptyFiltered : t.empty}</p>}

      {res.state === "ready" && groups.length > 0 && (
        <>
          <figure className="panel reveal" style={{ ["--i" as string]: 1 }}>
            <LineChart groups={groups} points={points} domain={domain} fmt={fmt} tickFmt={metric === "ms" ? fmtAxisMs : fmtAxisMiBs} unit={t.unit[metric]} />
          </figure>

          <div className="panel table-wrap reveal" style={{ ["--i" as string]: 2 }}>
            <table>
              <thead>
                <tr>
                  <th>{t.stack}</th>
                  <th>{t.platform}</th>
                  <th className="num">{t.runsCol}</th>
                  {STATES.map((s) => (
                    <th key={s} className="num" title={s}>
                      {t.states[s]}
                    </th>
                  ))}
                  <th className="num">{t.lastSeen}</th>
                </tr>
              </thead>
              <tbody>
                {groups
                  .map((g, cat) => ({ g, cat }))
                  .reverse()
                  .map(({ g, cat }) => {
                    const isOpen = open === g.key;
                    const runs = [...new Map(g.contribs.map((c) => [c.run.id, c])).values()].reverse();
                    return (
                      <Fragment key={g.key}>
                        <tr className={`row${isOpen ? " open" : ""}`} onClick={() => setOpen(isOpen ? null : g.key)}>
                          <td>
                            <button type="button" className="expand" aria-expanded={isOpen} aria-label={t.runsOf(g.label)}>
                              <svg viewBox="0 0 10 10" aria-hidden="true">
                                <path d="M3.5 2 6.5 5 3.5 8" fill="none" stroke="currentColor" strokeWidth="1.2" />
                              </svg>
                            </button>
                            <span className="ver" title={g.components.map((c) => `${c.component} ${c.tag}${c.version ? ` (${c.version})` : ""}`).join("\n")}>
                              {g.label}
                            </span>
                          </td>
                          <td className="muted plat">
                            {g.platforms.map((p, i) => (
                              <span key={p.key}>
                                {i > 0 && <span className="sep"> · </span>}
                                {`${p.classification}/${p.arch}`}
                              </span>
                            ))}
                          </td>
                          <td className="num">{g.runs}</td>
                          {STATES.map((s, si) => {
                            const v = value(cat, si);
                            const text = v == null ? "–" : fmt(v);
                            return (
                              <td key={s} className="num">
                                <span key={text} className="swap">
                                  {text}
                                </span>
                                <Delta v={v} base={cat > 0 ? value(cat - 1, si) : null} metric={metric} />
                              </td>
                            );
                          })}
                          <td className="num muted">{fmtDate(g.latest).slice(5, 16)}</td>
                        </tr>
                        <tr className={`members${isOpen ? " open" : ""}`} aria-hidden={!isOpen}>
                          <td colSpan={4 + STATES.length}>
                            <div className="collapse">
                              <ul>
                                {runs.map((c) => (
                                  <li key={c.run.id}>
                                    <code>{c.run.id}</code>
                                    <span className="muted">{`${c.run.host.classification} ${c.run.host.os}/${c.run.host.arch}`}</span>
                                    <Verdict verdict={c.report.verdict} label={t.verdicts[c.report.verdict]} />
                                    <span className="muted">{fmtDate(c.run.started_at)}</span>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          </td>
                        </tr>
                      </Fragment>
                    );
                  })}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}
