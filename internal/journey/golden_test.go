package journey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalGraphGolden(t *testing.T) {
	graph := canonicalGraph()
	if err := Validate(&graph); err != nil {
		t.Fatalf("canonical graph should validate: %v", err)
	}

	serialized, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		t.Fatalf("failed to serialize canonical graph: %v", err)
	}
	serialized = append(serialized, '\n')

	goldenPath := filepath.Join("testdata", "canonical_graph.json")
	if os.Getenv("UPDATE_GOLDEN") == "true" {
		_ = os.MkdirAll("testdata", 0755)
		_ = os.WriteFile(goldenPath, serialized, 0644)
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden file is missing: %s. Run with UPDATE_GOLDEN=true env var to generate.", goldenPath)
		}
		t.Fatalf("failed to read golden file: %v", err)
	}

	if string(serialized) != string(expected) {
		t.Errorf("golden mismatch!\nexpected:\n%s\ngot:\n%s", string(expected), string(serialized))
	}
}
