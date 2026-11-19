// Package api holds the API's contract, openapi.yaml. internal/api implements it; the CLI (apps/cli)
// builds its commands from it, read from GET /api/openapi.json.
package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"go.yaml.in/yaml/v3"
)

//go:embed openapi.yaml
var document []byte

// JSON is the contract as JSON. Keys keep the order openapi.yaml gives them, which is the order its
// authors chose: a client listing the fields of a schema shows them as written, not alphabetically.
var JSON = sync.OnceValues(func() ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(document, &root); err != nil {
		return nil, fmt.Errorf("read openapi.yaml: %w", err)
	}
	var out bytes.Buffer
	if err := writeJSON(&out, &root); err != nil {
		return nil, fmt.Errorf("openapi.yaml as JSON: %w", err)
	}
	return out.Bytes(), nil
})

func writeJSON(out *bytes.Buffer, node *yaml.Node) error {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return fmt.Errorf("empty document")
		}
		return writeJSON(out, node.Content[0])
	case yaml.AliasNode:
		return writeJSON(out, node.Alias)
	case yaml.MappingNode:
		out.WriteByte('{')
		for i := 0; i+1 < len(node.Content); i += 2 {
			if i > 0 {
				out.WriteByte(',')
			}
			// A key as written: an unquoted 200 is the response code "200", as JSON needs it.
			if err := writeValue(out, node.Content[i].Value); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := writeJSON(out, node.Content[i+1]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case yaml.SequenceNode:
		out.WriteByte('[')
		for i, item := range node.Content {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!null":
			out.WriteString("null")
		case "!!bool", "!!int", "!!float":
			var value any
			if err := node.Decode(&value); err != nil {
				return fmt.Errorf("line %d: %w", node.Line, err)
			}
			return writeValue(out, value)
		default:
			// Strings, and dates kept as the text they are: an example date is "2026-01-31", not a
			// timestamp at midnight UTC.
			return writeValue(out, node.Value)
		}
	default:
		return fmt.Errorf("line %d: unexpected YAML node kind %d", node.Line, node.Kind)
	}
	return nil
}

func writeValue(out *bytes.Buffer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %v: %w", value, err)
	}
	out.Write(encoded)
	return nil
}
