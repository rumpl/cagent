package apijson

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/openai/openai-go/v3/internal/encjson"
)

// MergeFields re-serializes the JSON object data with extra top-level fields
// added (or overwritten). It is how request params carry both their typed
// fields and caller-supplied extras such as `stream` or vendor-specific
// sampling knobs.
//
// The original key order is preserved and new keys are appended, so the body
// stays byte-identical to what the typed struct produced plus the additions.
func MergeFields(data []byte, extra map[string]any) ([]byte, error) {
	if len(extra) == 0 {
		return data, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("apijson: cannot add fields to non-object body")
	}

	var out bytes.Buffer
	out.WriteByte('{')
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyToken.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		if _, overridden := extra[key]; overridden {
			continue
		}
		if out.Len() > 1 {
			out.WriteByte(',')
		}
		encodedKey, err := encjson.Marshal(key)
		if err != nil {
			return nil, err
		}
		out.Write(encodedKey)
		out.WriteByte(':')
		out.Write(value)
	}

	for _, key := range slices.Sorted(maps.Keys(extra)) {
		encoded, err := encjson.Marshal(extra[key])
		if err != nil {
			return nil, err
		}
		if out.Len() > 1 {
			out.WriteByte(',')
		}
		encodedKey, err := encjson.Marshal(key)
		if err != nil {
			return nil, err
		}
		out.Write(encodedKey)
		out.WriteByte(':')
		out.Write(encoded)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
