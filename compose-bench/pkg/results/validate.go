package results

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Schema is the subset of JSON Schema used by schemas/*.schema.json:
// type, required, properties, items, enum, pattern and local $ref.
type Schema struct {
	Type       any                `json:"type"`
	Required   []string           `json:"required"`
	Properties map[string]*Schema `json:"properties"`
	Items      *Schema            `json:"items"`
	Enum       []any              `json:"enum"`
	Pattern    string             `json:"pattern"`
	Ref        string             `json:"$ref"`
	Defs       map[string]*Schema `json:"$defs"`
}

// LoadSchema reads a schema file.
func LoadSchema(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// Validate checks a decoded JSON value against the schema and returns all
// violations.
func (s *Schema) Validate(v any) []string {
	var errs []string
	s.validate(s, v, "$", &errs)
	return errs
}

func typeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case float64:
		if x == math.Trunc(x) {
			return "integer"
		}
		return "number"
	}
	return "unknown"
}

func (s *Schema) validate(root *Schema, v any, path string, errs *[]string) {
	if s.Ref != "" {
		name := strings.TrimPrefix(s.Ref, "#/$defs/")
		ref, ok := root.Defs[name]
		if !ok {
			*errs = append(*errs, path+": unknown $ref "+s.Ref)
			return
		}
		ref.validate(root, v, path, errs)
		return
	}
	if s.Type != nil {
		var types []string
		switch t := s.Type.(type) {
		case string:
			types = []string{t}
		case []any:
			for _, x := range t {
				types = append(types, x.(string))
			}
		}
		got := typeOf(v)
		ok := false
		for _, t := range types {
			if t == got || (t == "number" && got == "integer") {
				ok = true
			}
		}
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: type %s, want %v", path, got, types))
			return
		}
	}
	if len(s.Enum) > 0 {
		ok := false
		for _, e := range s.Enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				ok = true
			}
		}
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: %v not in enum %v", path, v, s.Enum))
		}
	}
	if s.Pattern != "" {
		if str, ok := v.(string); ok && !regexp.MustCompile(s.Pattern).MatchString(str) {
			*errs = append(*errs, fmt.Sprintf("%s: %q does not match %s", path, str, s.Pattern))
		}
	}
	switch x := v.(type) {
	case map[string]any:
		for _, r := range s.Required {
			if _, ok := x[r]; !ok {
				*errs = append(*errs, path+": missing required "+r)
			}
		}
		keys := make([]string, 0, len(s.Properties))
		for k := range s.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if pv, ok := x[k]; ok {
				s.Properties[k].validate(root, pv, path+"."+k, errs)
			}
		}
	case []any:
		if s.Items != nil {
			for i, item := range x {
				s.Items.validate(root, item, fmt.Sprintf("%s[%d]", path, i), errs)
			}
		}
	}
}

// ValidateResults validates every document under a results root (or a
// single run directory) and returns violations keyed by file.
func ValidateResults(schemaDir, root string) (map[string][]string, int, error) {
	load := func(n string) (*Schema, error) { return LoadSchema(filepath.Join(schemaDir, n+".schema.json")) }
	schemas := map[string]*Schema{}
	for _, n := range []string{"run", "block", "sample", "report", "index"} {
		s, err := load(n)
		if err != nil {
			return nil, 0, err
		}
		schemas[n] = s
	}
	out := map[string][]string{}
	checked := 0
	check := func(kind, path string, data []byte) {
		var v any
		checked++
		if err := json.Unmarshal(data, &v); err != nil {
			out[path] = append(out[path], err.Error())
			return
		}
		if errs := schemas[kind].Validate(v); len(errs) > 0 {
			out[path] = append(out[path], errs...)
		}
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		kind := map[string]string{"run.json": "run", "block.json": "block", "report.json": "report", "index.json": "index"}[info.Name()]
		if kind != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			check(kind, path, data)
		}
		if info.Name() == "samples.jsonl" {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<22)
			line := 0
			for sc.Scan() {
				line++
				check("sample", fmt.Sprintf("%s:%d", path, line), sc.Bytes())
			}
		}
		return nil
	})
	return out, checked, err
}
