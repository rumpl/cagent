// Package param provides the optional-value wrapper used by request
// parameters. A field of type Opt[T] distinguishes "unset" from "set to the
// zero value", which `omitzero` alone cannot express.
package param

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type status int8

const (
	omitted status = iota
	null
	included
)

// Opt represents an optional request parameter of type T.
type Opt[T comparable] struct {
	Value  T
	status status
}

// NewOpt returns an Opt holding v.
func NewOpt[T comparable](v T) Opt[T] { return Opt[T]{Value: v, status: included} }

// Null returns an Opt that serializes as the JSON value null.
func Null[T comparable]() Opt[T] { return Opt[T]{status: null} }

// Valid reports whether the value is present (neither omitted nor null).
func (o Opt[T]) Valid() bool {
	var empty Opt[T]
	return o.status == included || o != empty && o.status != null
}

// Or returns the value when valid, otherwise v.
func (o Opt[T]) Or(v T) T {
	if o.Valid() {
		return o.Value
	}
	return v
}

func (o Opt[T]) String() string {
	if o.status == null {
		return "null"
	}
	if s, ok := any(o.Value).(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprintf("%v", o.Value)
}

func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if !o.Valid() {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

func (o *Opt[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		o.status = null
		return nil
	}
	var value *T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value == nil {
		o.status = omitted
		return nil
	}
	o.status = included
	o.Value = *value
	return nil
}

// IsZero drives `json:",omitzero"`: an omitted Opt disappears from the wire,
// an explicitly null one is encoded as null.
func (o Opt[T]) IsZero() bool { return o == Opt[T]{} }

// IsOmitted reports whether v is unset and therefore omitted from a request.
func IsOmitted(v any) bool {
	if v == nil {
		return false
	}
	if z, ok := v.(interface{ IsZero() bool }); ok {
		return z.IsZero()
	}
	return reflect.ValueOf(v).IsZero()
}

// Optional is implemented by every Opt[T].
type Optional interface {
	Valid() bool
	IsZero() bool
}
