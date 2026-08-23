// Package param provides the optional-value wrapper used by request params.
package param

import (
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3/internal/encjson"
)

type status int8

const (
	omitted status = iota
	null
	included
)

// Opt represents an optional request parameter of type T. The zero value is
// omitted from the request body (the `omitzero` json tag relies on
// [Opt.IsZero]); an explicitly set value — including the zero value of T — is
// sent.
type Opt[T comparable] struct {
	Value  T
	status status
}

// NewOpt returns an Opt holding v.
func NewOpt[T comparable](v T) Opt[T] { return Opt[T]{Value: v, status: included} }

// Null returns an Opt that serializes as the JSON value null.
func Null[T comparable]() Opt[T] { return Opt[T]{status: null} }

// Valid reports whether the value was set and is not null.
func (o Opt[T]) Valid() bool { return o.status == included }

// Or returns the value when set, and v otherwise.
func (o Opt[T]) Or(v T) T {
	if o.Valid() {
		return o.Value
	}
	return v
}

// IsZero reports whether the field should be omitted; it is what the
// `json:",omitzero"` struct tags key off.
func (o Opt[T]) IsZero() bool { return o.status == omitted }

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
	return encjson.Marshal(o.Value)
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

// IsOmitted reports whether v is the zero (omitted) value of its type.
func IsOmitted[T comparable](v Opt[T]) bool { return v.IsZero() }
