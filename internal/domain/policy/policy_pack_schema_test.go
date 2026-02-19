package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestPolicyPackSamplesConformSchema(t *testing.T) {
	resolved := loadPolicyPackSchema(t)
	packs := mustGlob(t, filepath.Join(repoRoot(t), "policy", "packs", "*.json"))
	if len(packs) == 0 {
		t.Fatal("no policy packs found")
	}

	for _, packPath := range packs {
		t.Run(filepath.Base(packPath), func(t *testing.T) {
			instance := loadJSONValue(t, packPath)
			if err := resolved.Validate(instance); err != nil {
				t.Fatalf("policy pack schema validation failed: %v", err)
			}
		})
	}
}

func TestPolicyPackInvalidFixturesFailSchema(t *testing.T) {
	resolved := loadPolicyPackSchema(t)
	invalidFixtures := mustGlob(t, filepath.Join(repoRoot(t), "internal", "domain", "policy", "testdata", "invalid", "*.json"))
	if len(invalidFixtures) == 0 {
		t.Fatal("no invalid policy pack fixtures found")
	}

	for _, fixture := range invalidFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			instance := loadJSONValue(t, fixture)
			if err := resolved.Validate(instance); err == nil {
				t.Fatal("expected schema validation failure")
			}
		})
	}
}

func loadPolicyPackSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	schemaPath := filepath.Join(repoRoot(t), "policy", "schema", "policy-pack.schema.json")
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read policy pack schema: %v", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve schema: %v", err)
	}
	return resolved
}

func loadJSONValue(t *testing.T, path string) any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	return out
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %q: %v", pattern, err)
	}
	return matches
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
