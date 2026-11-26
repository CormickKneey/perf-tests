package stats

import (
	"math"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestDescribe(t *testing.T) {
	s := Describe([]float64{5, 1, 3, 2, 4})
	if s.N != 5 || s.Median != 3 || s.Min != 1 || s.Max != 5 || s.Mean != 3 {
		t.Fatalf("unexpected summary %+v", s)
	}
	if !near(s.P10, 1.4, 1e-9) || !near(s.P90, 4.6, 1e-9) {
		t.Fatalf("quantiles %+v", s)
	}
	if Describe(nil).N != 0 {
		t.Fatal("empty")
	}
}

func TestMannWhitneyExactSeparated(t *testing.T) {
	// Completely separated samples of 4 and 4: two-sided p = 2 / C(8,4) = 2/70.
	r := MannWhitney([]float64{1, 2, 3, 4}, []float64{5, 6, 7, 8})
	if r.Method != "exact" || r.U1 != 0 || r.U2 != 16 {
		t.Fatalf("unexpected %+v", r)
	}
	if !near(r.P, 2.0/70, 1e-12) {
		t.Fatalf("p = %v", r.P)
	}
}

func TestMannWhitneyExactKnown(t *testing.T) {
	// x = {1,3,5}, y = {2,4,6,8}: U1 = 0+1+2 = 3 (pairs x>y), U(min)=3.
	// Null distribution for n1=3,n2=4 has 35 arrangements; P(U<=3) = 7/35.
	r := MannWhitney([]float64{1, 3, 5}, []float64{2, 4, 6, 8})
	if r.U1 != 3 {
		t.Fatalf("U1 = %v", r.U1)
	}
	if !near(r.P, 2*7.0/35, 1e-12) {
		t.Fatalf("p = %v", r.P)
	}
}

func TestMannWhitneyIdentical(t *testing.T) {
	x := []float64{1, 1, 1, 1, 1}
	r := MannWhitney(x, x)
	if r.P != 1 {
		t.Fatalf("identical samples p = %v (%s)", r.P, r.Method)
	}
}

func TestMannWhitneyNormalWithTies(t *testing.T) {
	var x, y []float64
	for i := 0; i < 40; i++ {
		x = append(x, float64(i%10))
		y = append(y, float64(i%10)+5)
	}
	r := MannWhitney(x, y)
	if r.Method != "normal" {
		t.Fatalf("method %s", r.Method)
	}
	if r.P >= 1e-6 {
		t.Fatalf("shifted samples should be significant, p = %v", r.P)
	}
	same := MannWhitney(x, append([]float64(nil), x...))
	if same.P < 0.9 {
		t.Fatalf("same distribution p = %v", same.P)
	}
}

func TestMannWhitneySymmetric(t *testing.T) {
	x := []float64{1.1, 2.3, 3.7, 4.2, 9.9}
	y := []float64{2.2, 3.3, 5.1, 6.6}
	a, b := MannWhitney(x, y), MannWhitney(y, x)
	if !near(a.P, b.P, 1e-12) || a.U1 != b.U2 {
		t.Fatalf("asymmetric %+v %+v", a, b)
	}
}

func TestHolm(t *testing.T) {
	adj := Holm([]float64{0.01, 0.04, 0.03, 0.5})
	want := []float64{0.04, 0.09, 0.09, 0.5}
	for i := range want {
		if !near(adj[i], want[i], 1e-12) {
			t.Fatalf("adj = %v want %v", adj, want)
		}
	}
}

func TestRatioCIDeterministic(t *testing.T) {
	x := []float64{10, 11, 12, 13, 14, 15}
	y := []float64{20, 22, 24, 26, 28, 30}
	lo1, hi1 := RatioCI(x, y, 0.95, 500, 7)
	lo2, hi2 := RatioCI(x, y, 0.95, 500, 7)
	if lo1 != lo2 || hi1 != hi2 {
		t.Fatal("not deterministic")
	}
	if !(lo1 > 1.3 && hi1 < 2.8 && lo1 <= hi1) {
		t.Fatalf("ci = [%v, %v]", lo1, hi1)
	}
}
