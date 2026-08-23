// Package constant holds the single-valued discriminator types ("type",
// "role", "object", ...) carried by the API objects. A zero-valued constant
// still serializes as its documented value.
package constant

import "github.com/openai/openai-go/v3/internal/encjson"

func marshal[T ~string](value T, def string) ([]byte, error) {
	if value == "" {
		return encjson.Marshal(def)
	}
	return encjson.Marshal(string(value))
}

type Constant[T any] interface{ Default() T }

// ValueOf returns the default value of a constant type.
func ValueOf[T Constant[T]]() T {
	var t T
	return t.Default()
}

type (
	Assistant           string // Always "assistant"
	ChatCompletion      string // Always "chat.completion"
	ChatCompletionChunk string // Always "chat.completion.chunk"
	Embedding           string // Always "embedding"
	Explicit            string // Always "explicit"
	File                string // Always "file"
	Function            string // Always "function"
	FunctionCall        string // Always "function_call"
	FunctionCallOutput  string // Always "function_call_output"
	ImageURL            string // Always "image_url"
	InputAudio          string // Always "input_audio"
	InputFile           string // Always "input_file"
	InputImage          string // Always "input_image"
	InputText           string // Always "input_text"
	JSONSchema          string // Always "json_schema"
	List                string // Always "list"
	Model               string // Always "model"
	System              string // Always "system"
	Text                string // Always "text"
	Tool                string // Always "tool"
	ToolSearchCall      string // Always "tool_search_call"
	ToolSearchOutput    string // Always "tool_search_output"
	User                string // Always "user"
)

func (c Assistant) Default() Assistant                     { return "assistant" }
func (c ChatCompletion) Default() ChatCompletion           { return "chat.completion" }
func (c ChatCompletionChunk) Default() ChatCompletionChunk { return "chat.completion.chunk" }
func (c Embedding) Default() Embedding                     { return "embedding" }
func (c Explicit) Default() Explicit                       { return "explicit" }
func (c File) Default() File                               { return "file" }
func (c Function) Default() Function                       { return "function" }
func (c FunctionCall) Default() FunctionCall               { return "function_call" }
func (c FunctionCallOutput) Default() FunctionCallOutput   { return "function_call_output" }
func (c ImageURL) Default() ImageURL                       { return "image_url" }
func (c InputAudio) Default() InputAudio                   { return "input_audio" }
func (c InputFile) Default() InputFile                     { return "input_file" }
func (c InputImage) Default() InputImage                   { return "input_image" }
func (c InputText) Default() InputText                     { return "input_text" }
func (c JSONSchema) Default() JSONSchema                   { return "json_schema" }
func (c List) Default() List                               { return "list" }
func (c Model) Default() Model                             { return "model" }
func (c System) Default() System                           { return "system" }
func (c Text) Default() Text                               { return "text" }
func (c Tool) Default() Tool                               { return "tool" }
func (c ToolSearchCall) Default() ToolSearchCall           { return "tool_search_call" }
func (c ToolSearchOutput) Default() ToolSearchOutput       { return "tool_search_output" }
func (c User) Default() User                               { return "user" }

func (c Assistant) MarshalJSON() ([]byte, error) { return marshal(c, "assistant") }
func (c ChatCompletion) MarshalJSON() ([]byte, error) {
	return marshal(c, "chat.completion")
}

func (c ChatCompletionChunk) MarshalJSON() ([]byte, error) {
	return marshal(c, "chat.completion.chunk")
}
func (c Embedding) MarshalJSON() ([]byte, error)          { return marshal(c, "embedding") }
func (c Explicit) MarshalJSON() ([]byte, error)           { return marshal(c, "explicit") }
func (c File) MarshalJSON() ([]byte, error)               { return marshal(c, "file") }
func (c Function) MarshalJSON() ([]byte, error)           { return marshal(c, "function") }
func (c FunctionCall) MarshalJSON() ([]byte, error)       { return marshal(c, "function_call") }
func (c FunctionCallOutput) MarshalJSON() ([]byte, error) { return marshal(c, "function_call_output") }
func (c ImageURL) MarshalJSON() ([]byte, error)           { return marshal(c, "image_url") }
func (c InputAudio) MarshalJSON() ([]byte, error)         { return marshal(c, "input_audio") }
func (c InputFile) MarshalJSON() ([]byte, error)          { return marshal(c, "input_file") }
func (c InputImage) MarshalJSON() ([]byte, error)         { return marshal(c, "input_image") }
func (c InputText) MarshalJSON() ([]byte, error)          { return marshal(c, "input_text") }
func (c JSONSchema) MarshalJSON() ([]byte, error)         { return marshal(c, "json_schema") }
func (c List) MarshalJSON() ([]byte, error)               { return marshal(c, "list") }
func (c Model) MarshalJSON() ([]byte, error)              { return marshal(c, "model") }
func (c System) MarshalJSON() ([]byte, error)             { return marshal(c, "system") }
func (c Text) MarshalJSON() ([]byte, error)               { return marshal(c, "text") }
func (c Tool) MarshalJSON() ([]byte, error)               { return marshal(c, "tool") }
func (c ToolSearchCall) MarshalJSON() ([]byte, error)     { return marshal(c, "tool_search_call") }
func (c ToolSearchOutput) MarshalJSON() ([]byte, error)   { return marshal(c, "tool_search_output") }
func (c User) MarshalJSON() ([]byte, error)               { return marshal(c, "user") }
