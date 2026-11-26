package compose

import (
	"context"
	"testing"
	"time"
)

func TestValidateProject(t *testing.T) {
	good := []string{"dfperf-20261121t1020001a2b-baseline-b1", "dfperf-abcdefgh-candidate-b12"}
	bad := []string{"dfperf-abc-baseline-b1", "dfperf-ABCDEFGH-baseline-b1", "dfperf-abcdefgh-other-b1",
		"quant-rsi", "dfperf-abcdefgh-baseline-b", "dfperf-abcdefgh-baseline-b1-x", "xdfperf-abcdefgh-baseline-b1", ""}
	for _, g := range good {
		if err := ValidateProject(g); err != nil {
			t.Errorf("%s: %v", g, err)
		}
	}
	for _, b := range bad {
		if ValidateProject(b) == nil {
			t.Errorf("%q accepted", b)
		}
	}
}

func TestValidateFixturesVolume(t *testing.T) {
	if err := ValidateFixturesVolume("dfperf-fixtures-0123456789ab"); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"dfperf-fixtures-0123456789a", "dfperf-fixtures-0123456789AB", "dfperf-fixtures-xyz123456789", "fixtures"} {
		if ValidateFixturesVolume(b) == nil {
			t.Errorf("%q accepted", b)
		}
	}
}

func TestNewRunIDIsValid(t *testing.T) {
	id := NewRunID(time.Date(2026, 11, 21, 10, 20, 0, 0, time.UTC))
	if err := ValidateRunID(id); err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectName(id, "candidate", 3); err != nil {
		t.Fatal(err)
	}
}

func TestABBAOrder(t *testing.T) {
	o := ABBAOrder(4, true)
	if got := OrderString(o); got != "B C C B B C C B" {
		t.Fatalf("order %q", got)
	}
	counts := map[string]int{}
	for i, s := range o {
		if s.Order != i+1 {
			t.Fatalf("order index %d", s.Order)
		}
		counts[s.Variant]++
		if s.Index != counts[s.Variant] {
			t.Fatalf("block index %+v", s)
		}
	}
	if counts["baseline"] != 4 || counts["candidate"] != 4 {
		t.Fatalf("counts %v", counts)
	}
	if got := OrderString(ABBAOrder(2, false)); got != "B B" {
		t.Fatalf("smoke order %q", got)
	}
}

func TestCleanupRefusesForeignNames(t *testing.T) {
	// No docker command may run for names outside the dfperf pattern: the
	// Exec has no Log and would fail loudly if it were reached with a
	// cancelled context.
	x := &Exec{}
	for _, name := range []string{"quant-rsi", "multica", "dfperf-short-baseline-b1", "dfperf-abcdefgh-baseline-b1; rm -rf /"} {
		if err := removeProject(context.Background(), x, name); err == nil {
			t.Errorf("removeProject(%q) did not refuse", name)
		}
	}
	o := &Orchestrator{x: x}
	if err := o.down(context.Background(), "quant-rsi", nil, t.TempDir()); err == nil {
		t.Error("down accepted a foreign project")
	}
}
