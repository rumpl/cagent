package anthropic

import (
	"encoding/json"
	"reflect"
)

// marshalUnion serializes the single non-nil variant of a request union.
// Exactly one variant pointer is expected to be set; an empty union
// serializes as null.
func marshalUnion(variants ...any) ([]byte, error) {
	for _, v := range variants {
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && !rv.IsNil() {
			return json.Marshal(v)
		}
	}
	return []byte("null"), nil
}

// marshalWithExtras serializes v and merges extras into the resulting object,
// overriding any key already present.
func marshalWithExtras(v any, extras map[string]any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(extras) == 0 {
		return data, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	for k, v := range extras {
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		obj[k] = encoded
	}
	return json.Marshal(obj)
}

// paramUnion is an unexported marker embedded in every request union. Several
// unions are structurally identical (ToolUnionParam and
// MessageCountTokensToolUnionParam, for instance); the marker keeps them
// mutually inconvertible outside this package, as the generated SDK does.
type paramUnion struct{}
