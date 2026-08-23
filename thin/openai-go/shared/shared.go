// Package shared holds the types used by more than one API surface.
package shared

import (
	"github.com/openai/openai-go/v3/internal/encjson"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared/constant"
)

// ChatModel and ResponsesModel are plain model ids.
type (
	ChatModel      = string
	ResponsesModel = string
)

// FunctionParameters is a JSON Schema object describing a tool's arguments.
type FunctionParameters map[string]any

// Metadata is a free-form set of key/value pairs attached to an object.
type Metadata map[string]string

// FunctionDefinitionParam describes a callable function.
type FunctionDefinitionParam struct {
	Name        string             `json:"name"`
	Strict      param.Opt[bool]    `json:"strict,omitzero"`
	Description param.Opt[string]  `json:"description,omitzero"`
	Parameters  FunctionParameters `json:"parameters,omitzero"`
}

func (f FunctionDefinitionParam) IsZero() bool {
	return f.Name == "" && f.Strict.IsZero() && f.Description.IsZero() && f.Parameters == nil
}

// ReasoningEffort constrains how much a reasoning model thinks.
type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
)

// ReasoningSummary selects the granularity of the reasoning summary.
type ReasoningSummary string

const (
	ReasoningSummaryAuto     ReasoningSummary = "auto"
	ReasoningSummaryConcise  ReasoningSummary = "concise"
	ReasoningSummaryDetailed ReasoningSummary = "detailed"
)

// ReasoningParam configures reasoning on the Responses API.
type ReasoningParam struct {
	Effort  ReasoningEffort  `json:"effort,omitzero"`
	Summary ReasoningSummary `json:"summary,omitzero"`
}

func (r ReasoningParam) IsZero() bool { return r.Effort == "" && r.Summary == "" }

// ResponseFormatTextParam selects plain text output.
type ResponseFormatTextParam struct {
	Type constant.Text `json:"type"`
}

// ResponseFormatJSONObjectParam selects JSON-object output.
type ResponseFormatJSONObjectParam struct {
	Type string `json:"type"`
}

// ResponseFormatJSONSchemaParam selects structured output constrained by a schema.
type ResponseFormatJSONSchemaParam struct {
	JSONSchema ResponseFormatJSONSchemaJSONSchemaParam `json:"json_schema,omitzero"`
	Type       constant.JSONSchema                     `json:"type"`
}

// ResponseFormatJSONSchemaJSONSchemaParam is the schema itself, plus its metadata.
type ResponseFormatJSONSchemaJSONSchemaParam struct {
	Name        string            `json:"name"`
	Strict      param.Opt[bool]   `json:"strict,omitzero"`
	Description param.Opt[string] `json:"description,omitzero"`
	Schema      any               `json:"schema,omitzero"`
}

func (r ResponseFormatJSONSchemaJSONSchemaParam) IsZero() bool {
	return r.Name == "" && r.Strict.IsZero() && r.Description.IsZero() && r.Schema == nil
}

// ErrorObject is the error payload returned by the API.
type ErrorObject struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
	Type    string `json:"type"`
}

func (e ErrorObject) MarshalJSON() ([]byte, error) {
	type alias ErrorObject
	return encjson.Marshal(alias(e))
}
