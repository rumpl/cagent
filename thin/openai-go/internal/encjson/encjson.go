// Package encjson marshals JSON the way the API expects it: HTML characters
// (<, >, &) are left alone instead of being escaped to \u003c and friends, so
// prompts carrying markup reach the wire verbatim.
package encjson

import (
	"bytes"
	"encoding/json"
)

// Marshal encodes v without HTML escaping.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
