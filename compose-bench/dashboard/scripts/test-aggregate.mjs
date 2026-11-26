// Unit tests for src/aggregate.ts (node >= 22.18 strips the TypeScript types).
//   node scripts/test-aggregate.mjs
import assert from "node:assert/strict";
import { test } from "node:test";
import { applyStat, cell, contributions, groupStacks, percentile, platformsOf, shortRef, stackKey } from "../src/aggregate.ts";

test("percentile interpolates linearly", () => {
  assert.equal(percentile([], 0.99), null);
  assert.equal(percentile([7], 0.99), 7);
  // index = 0.99 * 1 = 0.99 -> 10 + 0.99 * 10
  assert.ok(Math.abs(percentile([20, 10], 0.99) - 19.9) < 1e-9);
  // n = 101: index = 99 exactly
  const xs = Array.from({ length: 101 }, (_, i) => 100 - i);
  assert.equal(percentile(xs, 0.99), 99);
});

test("applyStat", () => {
  const s = [3, 1, 2, 10];
  assert.equal(applyStat(s, "avg"), 4);
  assert.equal(applyStat(s, "max"), 10);
  assert.equal(applyStat(s, "min"), 1);
  assert.ok(Math.abs(applyStat(s, "p99") - (3 + 0.97 * 7)) < 1e-9);
  assert.equal(applyStat([], "avg"), null);
});

// Per-arch digests differ for the same release; the build version does not.
const img = (c, ver, arch) => ({
  component: c,
  ref: `dragonflyoss/${c}:${ver}`,
  pinned: `dragonflyoss/${c}@sha256:${arch}${ver}`.padEnd(40, "0"),
  image_id: "",
  platform: `linux/${arch}`,
  version: `${ver} (abc1234)`,
  source: "local",
});
const variant = (s, c, arch) => ({
  name: "x",
  scheduler: img("scheduler", s, arch),
  client: img("client", c, arch),
  driver: { ...img("client", c, arch), component: "driver", pinned: `dfperf-driver:${Math.random()}`, source: "derived" },
  overlays: null,
});
const run = (id, { mode = "smoke", host = "orbstack", arch = "arm64", s = "S1", c = "C1", cs, cc, verdict = "PASS", profile = "file-3peers" } = {}) => ({
  id,
  mode,
  profile,
  baseline: variant(s, c, arch),
  candidate: mode === "compare" ? variant(cs ?? s, cc ?? c, arch) : undefined,
  host: { classification: host, os: host === "orbstack" ? "darwin" : "linux", arch },
  non_baseline: host !== "linux-native",
  started_at: `2026-11-2${id}T00:00:00Z`,
  verdict,
});
const report = (b, c) => ({
  verdict: "PASS",
  groups: b ? [{ state: "cold", file: "f.bin", size_bytes: 1048576, baseline_durations_ms: b, candidate_durations_ms: c ?? null }] : null,
});

test("stack key ignores mode, host, arch and the derived driver", () => {
  const a = run("1");
  const b = run("2", { mode: "compare", host: "linux-native", arch: "amd64" });
  assert.equal(stackKey(a.baseline), stackKey(b.baseline));
  assert.equal(stackKey(a.baseline), stackKey(b.candidate));
  assert.notEqual(stackKey(a.baseline), stackKey(run("3", { c: "C2" }).baseline));
  assert.equal(stackKey(a.baseline), "scheduler=S1 (abc1234)|client=C1 (abc1234)");
  assert.equal(shortRef("dragonflyoss/scheduler:v2.5.4"), "v2.5.4");
  assert.equal(shortRef("registry:5000/dragonflyoss/client@sha256:d942d337b7c6"), "@d942d33");
});

test("all runs of a stack pool into one point; invalid excluded; p99 over concatenated samples", () => {
  const members = [
    { run: run("1"), report: report([100, 200]) },
    { run: run("2", { host: "linux-native", arch: "amd64" }), report: report([300]) },
    { run: run("3", { mode: "compare" }), report: report([10, 20], [1000, 2000]) }, // A/A
    { run: run("4", { verdict: "INVALID" }), report: report(null) },
  ];
  const groups = groupStacks(members);
  assert.equal(groups.length, 1);
  const [g] = groups;
  assert.equal(g.runs, 3);
  assert.equal(g.label, "S1 · C1");
  assert.deepEqual(
    g.platforms.map((p) => [p.key, p.runs]),
    [
      ["linux-native linux/amd64", 1],
      ["orbstack darwin/arm64", 2],
    ],
  );
  const all = [100, 200, 300, 10, 20, 1000, 2000];
  assert.deepEqual(cell(g.contribs, "cold", "f.bin", "ms", "avg"), { value: all.reduce((s, v) => s + v) / all.length, n: 7, runs: 3 });
  assert.equal(cell(g.contribs, "cold", "f.bin", "ms", "p99").value, percentile(all, 0.99));
  assert.equal(cell(g.contribs, "cold", "f.bin", "ms", "min").value, 10);
  assert.equal(cell(g.contribs, "cold", "f.bin", "mibs", "max").value, 100);
  assert.equal(cell(g.contribs, "warm", "f.bin", "ms", "avg").value, null);
});

test("compare run feeds baseline and candidate stacks separately, ordered by first seen", () => {
  const members = [
    { run: run("1"), report: report([100]) },
    { run: run("2", { mode: "compare", cc: "C2" }), report: report([10, 20], [1000, 2000]) },
    { run: run("3", { c: "C2" }), report: report([3000]) },
  ];
  assert.equal(contributions(members).length, 4);
  const groups = groupStacks(members);
  assert.deepEqual(
    groups.map((g) => g.label),
    ["S1 · C1", "S1 · C2"],
  );
  const [base, cand] = groups;
  assert.deepEqual(cell(base.contribs, "cold", "f.bin", "ms", "max"), { value: 100, n: 3, runs: 2 });
  assert.deepEqual(cell(cand.contribs, "cold", "f.bin", "ms", "min"), { value: 1000, n: 3, runs: 2 });
  assert.equal(cand.first, "2026-11-22T00:00:00Z");
  assert.equal(cand.latest, "2026-11-23T00:00:00Z");
});

test("platform filter narrows the pool without changing the key", () => {
  const members = [
    { run: run("1"), report: report([100]) },
    { run: run("2", { host: "linux-native", arch: "amd64" }), report: report([300]) },
  ];
  assert.deepEqual(
    platformsOf(members).map((p) => p.key),
    ["linux-native linux/amd64", "orbstack darwin/arm64"],
  );
  const [g] = groupStacks(members, { platform: "linux-native linux/amd64" });
  assert.equal(g.key, groupStacks(members)[0].key);
  assert.equal(cell(g.contribs, "cold", "f.bin", "ms", "avg").value, 300);
  assert.equal(groupStacks(members, { profile: "other" }).length, 0);
});
