// Package compose orchestrates benchmark blocks with docker compose on the
// host. Every destructive operation is guarded by the name rules below.
package compose

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	projectRe  = regexp.MustCompile(`^dfperf-[0-9a-z]{8,}-(baseline|candidate)-b[0-9]+$`)
	fixturesRe = regexp.MustCompile(`^dfperf-fixtures-[0-9a-f]{12,64}$`)
	runIDRe    = regexp.MustCompile(`^[0-9a-z]{8,}$`)
)

// ValidateProject returns an error unless name is a dfperf block project.
func ValidateProject(name string) error {
	if !projectRe.MatchString(name) {
		return fmt.Errorf("refusing to touch compose project %q: does not match %s", name, projectRe)
	}
	return nil
}

// ValidateFixturesVolume returns an error unless name is a dfperf fixtures volume.
func ValidateFixturesVolume(name string) error {
	if !fixturesRe.MatchString(name) {
		return fmt.Errorf("refusing to touch volume %q: does not match %s", name, fixturesRe)
	}
	return nil
}

// ValidateRunID checks a run id.
func ValidateRunID(id string) error {
	if !runIDRe.MatchString(id) {
		return fmt.Errorf("invalid run id %q: must match %s", id, runIDRe)
	}
	return nil
}

// NewRunID returns a sortable, lowercase alphanumeric run id.
func NewRunID(now time.Time) string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102t150405") + hex.EncodeToString(b[:])
}

// ProjectName builds and validates a block project name.
func ProjectName(runID, variant string, block int) (string, error) {
	name := fmt.Sprintf("dfperf-%s-%s-b%d", runID, variant, block)
	return name, ValidateProject(name)
}

// BlockSlot is one entry of the execution order.
type BlockSlot struct {
	Order   int    `json:"order"`
	Variant string `json:"variant"`
	Index   int    `json:"index"`
}

// ID is the directory name of a block.
func (b BlockSlot) ID() string { return fmt.Sprintf("%02d-%s-b%d", b.Order, b.Variant, b.Index) }

// ABBAOrder returns the interleaved order A B B A A B B A ... with n blocks
// per variant. With candidate == "" only baseline blocks are produced.
func ABBAOrder(n int, withCandidate bool) []BlockSlot {
	var out []BlockSlot
	if !withCandidate {
		for i := 1; i <= n; i++ {
			out = append(out, BlockSlot{Order: i, Variant: "baseline", Index: i})
		}
		return out
	}
	counts := map[string]int{}
	add := func(v string) {
		counts[v]++
		out = append(out, BlockSlot{Order: len(out) + 1, Variant: v, Index: counts[v]})
	}
	for pair := 0; pair < n; pair++ {
		if pair%2 == 0 {
			add("baseline")
			add("candidate")
		} else {
			add("candidate")
			add("baseline")
		}
	}
	return out
}

// OrderString renders the order as "A B B A ...".
func OrderString(slots []BlockSlot) string {
	parts := make([]string, len(slots))
	for i, s := range slots {
		parts[i] = strings.ToUpper(s.Variant[:1])
	}
	return strings.Join(parts, " ")
}
