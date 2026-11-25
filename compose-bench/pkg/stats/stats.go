// Package stats implements the small amount of statistics the comparison
// needs: descriptive statistics, the Mann–Whitney U test (exact when there
// are no ties and the samples are small, otherwise the tie-corrected normal
// approximation with continuity correction), Holm–Bonferroni correction and a
// deterministic bootstrap CI for the ratio of medians.
package stats

import (
	"math"
	"math/rand/v2"
	"sort"
)

// Summary holds descriptive statistics of one sample set.
type Summary struct {
	N      int     `json:"n"`
	Min    float64 `json:"min"`
	P10    float64 `json:"p10"`
	Median float64 `json:"median"`
	Mean   float64 `json:"mean"`
	P90    float64 `json:"p90"`
	Max    float64 `json:"max"`
	Stddev float64 `json:"stddev"`
	CV     float64 `json:"cv"`
}

// Describe computes a Summary. An empty input yields N == 0.
func Describe(xs []float64) Summary {
	if len(xs) == 0 {
		return Summary{}
	}
	s := sorted(xs)
	var sum float64
	for _, x := range s {
		sum += x
	}
	mean := sum / float64(len(s))
	var ss float64
	for _, x := range s {
		ss += (x - mean) * (x - mean)
	}
	sd := 0.0
	if len(s) > 1 {
		sd = math.Sqrt(ss / float64(len(s)-1))
	}
	cv := 0.0
	if mean != 0 {
		cv = sd / mean
	}
	return Summary{
		N: len(s), Min: s[0], Max: s[len(s)-1], Mean: mean, Stddev: sd, CV: cv,
		P10: quantileSorted(s, 0.10), Median: quantileSorted(s, 0.5), P90: quantileSorted(s, 0.90),
	}
}

// Median returns the median (NaN for empty input).
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	return quantileSorted(sorted(xs), 0.5)
}

// Quantile uses linear interpolation between closest ranks (type 7).
func Quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	return quantileSorted(sorted(xs), q)
}

func quantileSorted(s []float64, q float64) float64 {
	if len(s) == 1 {
		return s[0]
	}
	pos := q * float64(len(s)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	frac := pos - float64(lo)
	return s[lo] + (s[hi]-s[lo])*frac
}

func sorted(xs []float64) []float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s
}

// MWResult is the outcome of a two-sided Mann–Whitney U test.
type MWResult struct {
	U1     float64 `json:"u1"`
	U2     float64 `json:"u2"`
	P      float64 `json:"p"`
	Method string  `json:"method"`
	// Effect is the common-language effect size P(X > Y) + 0.5 P(X = Y).
	Effect float64 `json:"effect"`
}

// exactLimit bounds the size for which the exact null distribution is used.
const exactLimit = 50

// MannWhitney performs a two-sided test of x vs y.
func MannWhitney(x, y []float64) MWResult {
	n1, n2 := len(x), len(y)
	if n1 == 0 || n2 == 0 {
		return MWResult{P: 1, Method: "none"}
	}
	type obs struct {
		v     float64
		group int
	}
	all := make([]obs, 0, n1+n2)
	for _, v := range x {
		all = append(all, obs{v, 0})
	}
	for _, v := range y {
		all = append(all, obs{v, 1})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v < all[j].v })
	ranks := make([]float64, len(all))
	var tieTerm float64
	ties := false
	for i := 0; i < len(all); {
		j := i
		for j+1 < len(all) && all[j+1].v == all[i].v {
			j++
		}
		r := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			ranks[k] = r
		}
		if t := float64(j - i + 1); t > 1 {
			ties = true
			tieTerm += t*t*t - t
		}
		i = j + 1
	}
	var r1 float64
	for i, o := range all {
		if o.group == 0 {
			r1 += ranks[i]
		}
	}
	fn1, fn2 := float64(n1), float64(n2)
	u1 := r1 - fn1*(fn1+1)/2
	u2 := fn1*fn2 - u1
	res := MWResult{U1: u1, U2: u2, Effect: u1 / (fn1 * fn2)}
	if !ties && n1+n2 <= exactLimit {
		res.P = exactP(n1, n2, math.Min(u1, u2))
		res.Method = "exact"
		return res
	}
	n := fn1 + fn2
	mu := fn1 * fn2 / 2
	sigma2 := fn1 * fn2 / 12 * ((n + 1) - tieTerm/(n*(n-1)))
	if sigma2 <= 0 {
		res.P, res.Method = 1, "normal"
		return res
	}
	z := (math.Abs(u1-mu) - 0.5) / math.Sqrt(sigma2)
	if z < 0 {
		z = 0
	}
	res.P = math.Min(1, 2*(1-normCDF(z)))
	res.Method = "normal"
	return res
}

// exactP returns the two-sided p-value P(U <= u) * 2 under H0 for small
// samples without ties, by counting rank arrangements with a DP.
func exactP(n1, n2 int, u float64) float64 {
	// count[i][j][k]: arrangements of i x's and j y's with U == k.
	maxU := n1 * n2
	prev := make([][]float64, n2+1)
	for j := range prev {
		prev[j] = make([]float64, maxU+1)
		prev[j][0] = 1 // i == 0: U == 0
	}
	for i := 1; i <= n1; i++ {
		cur := make([][]float64, n2+1)
		for j := 0; j <= n2; j++ {
			cur[j] = make([]float64, maxU+1)
			for k := 0; k <= maxU; k++ {
				// last element is an x (contributes j to U) or a y.
				var v float64
				if k-j >= 0 {
					v += prev[j][k-j]
				}
				if j > 0 {
					v += cur[j-1][k]
				}
				cur[j][k] = v
			}
		}
		prev = cur
	}
	counts := prev[n2]
	var total, tail float64
	for k, c := range counts {
		total += c
		if float64(k) <= u+1e-9 {
			tail += c
		}
	}
	return math.Min(1, 2*tail/total)
}

func normCDF(z float64) float64 { return 0.5 * math.Erfc(-z/math.Sqrt2) }

// Holm applies the Holm–Bonferroni step-down correction and returns adjusted
// p-values in the input order.
func Holm(ps []float64) []float64 {
	m := len(ps)
	idx := make([]int, m)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return ps[idx[a]] < ps[idx[b]] })
	adj := make([]float64, m)
	running := 0.0
	for rank, i := range idx {
		v := math.Min(1, float64(m-rank)*ps[i])
		if v < running {
			v = running
		}
		running = v
		adj[i] = v
	}
	return adj
}

// RatioCI returns a deterministic percentile bootstrap confidence interval of
// median(y)/median(x).
func RatioCI(x, y []float64, level float64, iters int, seed uint64) (lo, hi float64) {
	if len(x) == 0 || len(y) == 0 {
		return math.NaN(), math.NaN()
	}
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	ratios := make([]float64, 0, iters)
	bx := make([]float64, len(x))
	by := make([]float64, len(y))
	for i := 0; i < iters; i++ {
		for k := range bx {
			bx[k] = x[rng.IntN(len(x))]
		}
		for k := range by {
			by[k] = y[rng.IntN(len(y))]
		}
		mx := Median(bx)
		if mx == 0 {
			continue
		}
		ratios = append(ratios, Median(by)/mx)
	}
	if len(ratios) == 0 {
		return math.NaN(), math.NaN()
	}
	a := (1 - level) / 2
	return Quantile(ratios, a), Quantile(ratios, 1-a)
}
