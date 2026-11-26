package results

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSchemaValidator(t *testing.T) {
	s, err := LoadSchema("../../schemas/block.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	good := `{"schema":"dfperf/v1","run":"r","id":"01-baseline-b1","project":"dfperf-abcdefgh-baseline-b1","variant":"baseline",
	"index":1,"order":1,"status":"ok","checks":[{"name":"x","ok":true,"required":true,"detail":""}],"samples":3,"failures":0,"runner_exit_code":0}`
	var v any
	json.Unmarshal([]byte(good), &v)
	if errs := s.Validate(v); len(errs) != 0 {
		t.Fatalf("valid doc rejected: %v", errs)
	}
	bad := `{"schema":"dfperf/v2","run":"r","id":"x","project":"quant-rsi","variant":"other","index":1.5,"order":1,"status":"ok",
	"checks":[{"name":"x"}],"samples":3,"failures":0}`
	json.Unmarshal([]byte(bad), &v)
	errs := s.Validate(v)
	if len(errs) < 6 {
		t.Fatalf("expected many violations, got %v", errs)
	}
}

// TestCommittedResultsMatchSchemas validates the real results/ tree when
// present (it is committed as evidence).
func TestCommittedResultsMatchSchemas(t *testing.T) {
	root := "../../results"
	if _, err := os.Stat(filepath.Join(root, "index.json")); err != nil {
		t.Skip("no results yet")
	}
	bad, n, err := ValidateResults("../../schemas", root)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("nothing validated")
	}
	for f, errs := range bad {
		t.Errorf("%s: %v", f, errs)
	}
}
