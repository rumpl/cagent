// Package apijson implements the reflective response decoder used by the
// generated-looking response structs: it decodes with encoding/json and
// additionally records, for every field, whether the server actually sent it.
package apijson

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/openai/openai-go/v3/packages/respjson"
)

var fieldType = reflect.TypeFor[respjson.Field]()

// UnmarshalRoot decodes data into ptr (a pointer to a struct) and fills its
// optional `JSON` metadata struct: one [respjson.Field] per exported field,
// plus ExtraFields for unknown keys and Raw for the whole object.
func UnmarshalRoot(data []byte, ptr any) error {
	rv := reflect.ValueOf(ptr)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return json.Unmarshal(data, ptr)
	}
	sv := rv.Elem()
	if sv.Kind() != reflect.Struct {
		return json.Unmarshal(data, ptr)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	meta := sv.FieldByName("JSON")
	hasMeta := meta.IsValid() && meta.Kind() == reflect.Struct
	if hasMeta {
		if f := meta.FieldByName("Raw"); f.IsValid() && f.Kind() == reflect.String && f.CanSet() {
			f.SetString(string(data))
		}
	}

	st := sv.Type()
	known := make(map[string]bool, st.NumField())
	for i := range st.NumField() {
		field := st.Field(i)
		name, ok := jsonName(field)
		if !ok {
			continue
		}
		known[name] = true
		value, present := raw[name]
		if !present {
			continue
		}
		status := respjson.NewField(string(value))
		if err := json.Unmarshal(value, sv.Field(i).Addr().Interface()); err != nil {
			// A single malformed field must not lose the rest of the object:
			// providers routinely send extra or differently-typed values.
			status = respjson.NewInvalidField(string(value))
		}
		if hasMeta {
			if f := meta.FieldByName(field.Name); f.IsValid() && f.Type() == fieldType && f.CanSet() {
				f.Set(reflect.ValueOf(status))
			}
		}
	}

	if hasMeta {
		if extra := meta.FieldByName("ExtraFields"); extra.IsValid() && extra.Kind() == reflect.Map && extra.CanSet() {
			out := reflect.MakeMap(extra.Type())
			for key, value := range raw {
				if known[key] {
					continue
				}
				out.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(respjson.NewField(string(value))))
			}
			extra.Set(out)
		}
	}

	return nil
}

func jsonName(field reflect.StructField) (string, bool) {
	if field.PkgPath != "" {
		return "", false
	}
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return "", false
	}
	return name, true
}
