// Package respjson carries per-field metadata about a decoded response, so a
// caller can tell a JSON null or an omitted field apart from the zero value.
package respjson

type status int8

const (
	omitted status = iota
	null
	invalid
	valid
)

// Field describes the presence of one field of a decoded response object.
type Field struct {
	status status
	raw    string
}

// Valid reports whether the field was present and not null.
func (f Field) Valid() bool { return f.status > invalid }

// Raw returns the raw JSON of the field, or "" when it was omitted.
func (f Field) Raw() string {
	if f.status == omitted {
		return ""
	}
	return f.raw
}

const (
	Null    string = "null"
	Omitted string = ""
)

// NewField builds a Field from the raw JSON of a present field.
func NewField(raw string) Field {
	if raw == Null {
		return Field{status: null, raw: Null}
	}
	return Field{status: valid, raw: raw}
}

// NewInvalidField builds a Field for a value that failed to decode.
func NewInvalidField(raw string) Field { return Field{status: invalid, raw: raw} }
