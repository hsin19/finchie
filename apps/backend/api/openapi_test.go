package api

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestJSONIsTheContractWithItsKeysInOrder(t *testing.T) {
	asJSON, err := JSON()
	if err != nil {
		t.Fatal(err)
	}

	var spec struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(asJSON, &spec); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if spec.OpenAPI == "" || spec.Paths["/api/openapi.json"] == nil {
		t.Fatalf("openapi = %q, %d paths, /api/openapi.json missing or not", spec.OpenAPI, len(spec.Paths))
	}

	// The top level as openapi.yaml writes it, not sorted.
	var top yaml.Node
	if err := yaml.Unmarshal(document, &top); err != nil {
		t.Fatal(err)
	}
	var want []string
	for i := 0; i < len(top.Content[0].Content); i += 2 {
		want = append(want, top.Content[0].Content[i].Value)
	}
	if got := keys(t, asJSON); !slices.Equal(got, want) {
		t.Fatalf("top-level keys = %v, want %v", got, want)
	}
}

func TestJSONKeepsScalarsAsWritten(t *testing.T) {
	var root yaml.Node
	source := []byte(`
responses:
  200: { description: ok }
example: 2026-01-31
limit: 500
exact: true
amount: "-6552.97"
none: ~
`)
	if err := yaml.Unmarshal(source, &root); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeJSON(&out, &root); err != nil {
		t.Fatal(err)
	}
	want := `{"responses":{"200":{"description":"ok"}},"example":"2026-01-31","limit":500,"exact":true,"amount":"-6552.97","none":null}`
	if out.String() != want {
		t.Fatalf("got  %s\nwant %s", out.String(), want)
	}
}

func keys(t *testing.T, object []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(object))
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	var names []string
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, name.(string))
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return names
}
