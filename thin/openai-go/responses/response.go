// Package responses implements the OpenAI Responses API surface: the request
// params, the input item unions, and the streamed events.
package responses

import (
	"context"
	"net/http"

	"github.com/openai/openai-go/v3/internal/apijson"
	"github.com/openai/openai-go/v3/internal/encjson"
	"github.com/openai/openai-go/v3/internal/requestconfig"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
)

// Aliases for the shared types reachable through this package.
type (
	FunctionDefinitionParam                 = shared.FunctionDefinitionParam
	ReasoningEffort                         = shared.ReasoningEffort
	ReasoningParam                          = shared.ReasoningParam
	ResponseFormatJSONObjectParam           = shared.ResponseFormatJSONObjectParam
	ResponseFormatJSONSchemaParam           = shared.ResponseFormatJSONSchemaParam
	ResponseFormatJSONSchemaJSONSchemaParam = shared.ResponseFormatJSONSchemaJSONSchemaParam
	ResponseFormatTextParam                 = shared.ResponseFormatTextParam
	ResponsesModel                          = shared.ResponsesModel
)

// ResponseService talks to POST /responses.
type ResponseService struct {
	Options []option.RequestOption
}

func NewResponseService(opts ...option.RequestOption) ResponseService {
	return ResponseService{Options: opts}
}

// New creates a response and waits for the full result.
func (r ResponseService) New(ctx context.Context, body ResponseNewParams, opts ...option.RequestOption) (*Response, error) {
	encoded, err := encjson.Marshal(body)
	if err != nil {
		return nil, err
	}
	res := &Response{}
	err = requestconfig.ExecuteNewRequest(ctx, http.MethodPost, "responses", encoded, res,
		append(r.Options[:len(r.Options):len(r.Options)], opts...)...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// NewStreaming creates a response streamed as server-sent events. A request
// failure is carried by the returned stream's Err.
func (r ResponseService) NewStreaming(ctx context.Context, body ResponseNewParams, opts ...option.RequestOption) *ssestream.Stream[ResponseStreamEventUnion] {
	var raw *http.Response
	encoded, err := encjson.Marshal(body)
	if err == nil {
		encoded, err = apijson.MergeFields(encoded, map[string]any{"stream": true})
	}
	if err == nil {
		err = requestconfig.ExecuteNewRequest(ctx, http.MethodPost, "responses", encoded, &raw,
			append(r.Options[:len(r.Options):len(r.Options)], opts...)...)
	}
	return ssestream.NewStream[ResponseStreamEventUnion](ssestream.NewDecoder(raw), err)
}

// --- request params -------------------------------------------------------

// ResponseNewParams is the body of a responses request.
type ResponseNewParams struct {
	Instructions       param.Opt[string]                `json:"instructions,omitzero"`
	MaxOutputTokens    param.Opt[int64]                 `json:"max_output_tokens,omitzero"`
	MaxToolCalls       param.Opt[int64]                 `json:"max_tool_calls,omitzero"`
	ParallelToolCalls  param.Opt[bool]                  `json:"parallel_tool_calls,omitzero"`
	PreviousResponseID param.Opt[string]                `json:"previous_response_id,omitzero"`
	PromptCacheKey     param.Opt[string]                `json:"prompt_cache_key,omitzero"`
	Store              param.Opt[bool]                  `json:"store,omitzero"`
	Temperature        param.Opt[float64]               `json:"temperature,omitzero"`
	TopP               param.Opt[float64]               `json:"top_p,omitzero"`
	User               param.Opt[string]                `json:"user,omitzero"`
	Include            []string                         `json:"include,omitzero"`
	Metadata           shared.Metadata                  `json:"metadata,omitzero"`
	Truncation         string                           `json:"truncation,omitzero"`
	Input              ResponseNewParamsInputUnion      `json:"input,omitzero"`
	Model              shared.ResponsesModel            `json:"model,omitzero"`
	Reasoning          shared.ReasoningParam            `json:"reasoning,omitzero"`
	Text               ResponseTextConfigParam          `json:"text,omitzero"`
	ToolChoice         ResponseNewParamsToolChoiceUnion `json:"tool_choice,omitzero"`
	Tools              []ToolUnionParam                 `json:"tools,omitzero"`

	extraFields map[string]any
}

// SetExtraFields adds body fields the typed struct doesn't model.
func (p *ResponseNewParams) SetExtraFields(fields map[string]any) { p.extraFields = fields }

func (p ResponseNewParams) MarshalJSON() ([]byte, error) {
	type alias ResponseNewParams
	encoded, err := encjson.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return apijson.MergeFields(encoded, p.extraFields)
}

// ResponseInputParam is the conversation sent to the model.
type ResponseInputParam []ResponseInputItemUnionParam

// ResponseNewParamsInputUnion is either a bare prompt or a list of items.
type ResponseNewParamsInputUnion struct {
	OfString        param.Opt[string]
	OfInputItemList ResponseInputParam
}

func (u ResponseNewParamsInputUnion) IsZero() bool {
	return u.OfString.IsZero() && u.OfInputItemList == nil
}

func (u ResponseNewParamsInputUnion) MarshalJSON() ([]byte, error) {
	if u.OfString.Valid() {
		return encjson.Marshal(u.OfString)
	}
	if u.OfInputItemList != nil {
		return encjson.Marshal(u.OfInputItemList)
	}
	return []byte("null"), nil
}

// ToolChoiceOptions is the tool-choice mode.
type ToolChoiceOptions string

const (
	ToolChoiceOptionsNone     ToolChoiceOptions = "none"
	ToolChoiceOptionsAuto     ToolChoiceOptions = "auto"
	ToolChoiceOptionsRequired ToolChoiceOptions = "required"
)

// ResponseNewParamsToolChoiceUnion is either a mode or a specific tool.
type ResponseNewParamsToolChoiceUnion struct {
	OfToolChoiceMode param.Opt[ToolChoiceOptions]
	OfFunctionTool   *ToolChoiceFunctionParam
}

func (u ResponseNewParamsToolChoiceUnion) IsZero() bool {
	return u.OfToolChoiceMode.IsZero() && u.OfFunctionTool == nil
}

func (u ResponseNewParamsToolChoiceUnion) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfToolChoiceMode.Valid():
		return encjson.Marshal(u.OfToolChoiceMode)
	case u.OfFunctionTool != nil:
		return encjson.Marshal(u.OfFunctionTool)
	}
	return []byte("null"), nil
}

// ToolChoiceFunctionParam forces one function.
type ToolChoiceFunctionParam struct {
	Name string            `json:"name"`
	Type constant.Function `json:"type"`
}

// ResponseTextConfigParam configures the text output.
type ResponseTextConfigParam struct {
	Verbosity string                             `json:"verbosity,omitzero"`
	Format    ResponseFormatTextConfigUnionParam `json:"format,omitzero"`
}

func (p ResponseTextConfigParam) IsZero() bool { return p.Verbosity == "" && p.Format.IsZero() }

// ResponseFormatTextConfigUnionParam selects the output format.
type ResponseFormatTextConfigUnionParam struct {
	OfText       *shared.ResponseFormatTextParam
	OfJSONSchema *ResponseFormatTextJSONSchemaConfigParam
	OfJSONObject *shared.ResponseFormatJSONObjectParam
}

func (u ResponseFormatTextConfigUnionParam) IsZero() bool {
	return u.OfText == nil && u.OfJSONSchema == nil && u.OfJSONObject == nil
}

func (u ResponseFormatTextConfigUnionParam) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfText != nil:
		return encjson.Marshal(u.OfText)
	case u.OfJSONSchema != nil:
		return encjson.Marshal(u.OfJSONSchema)
	case u.OfJSONObject != nil:
		return encjson.Marshal(u.OfJSONObject)
	}
	return []byte("null"), nil
}

// ResponseFormatTextJSONSchemaConfigParam constrains the output to a schema.
type ResponseFormatTextJSONSchemaConfigParam struct {
	Name        string              `json:"name"`
	Schema      map[string]any      `json:"schema,omitzero"`
	Strict      param.Opt[bool]     `json:"strict,omitzero"`
	Description param.Opt[string]   `json:"description,omitzero"`
	Type        constant.JSONSchema `json:"type"`
}

// --- tools ----------------------------------------------------------------

// ToolUnionParam is one tool exposed to the model.
type ToolUnionParam struct {
	OfFunction *FunctionToolParam
}

func (u ToolUnionParam) IsZero() bool { return u.OfFunction == nil }

func (u ToolUnionParam) MarshalJSON() ([]byte, error) {
	if u.OfFunction != nil {
		return encjson.Marshal(u.OfFunction)
	}
	return []byte("null"), nil
}

// FunctionToolParam describes a callable function tool.
type FunctionToolParam struct {
	Strict         param.Opt[bool]   `json:"strict,omitzero"`
	Parameters     map[string]any    `json:"parameters,omitzero"`
	Name           string            `json:"name"`
	Description    param.Opt[string] `json:"description,omitzero"`
	DeferLoading   param.Opt[bool]   `json:"defer_loading,omitzero"`
	AllowedCallers []string          `json:"allowed_callers,omitzero"`
	OutputSchema   map[string]any    `json:"output_schema,omitzero"`
	Type           constant.Function `json:"type"`
}

// --- input items ----------------------------------------------------------

// ResponseInputItemUnionParam is one item of the conversation; exactly one
// variant is set.
type ResponseInputItemUnionParam struct {
	OfMessage            *EasyInputMessageParam
	OfInputMessage       *ResponseInputItemMessageParam
	OfFunctionCall       *ResponseFunctionToolCallParam
	OfFunctionCallOutput *ResponseInputItemFunctionCallOutputParam
	OfToolSearchCall     *ResponseInputItemToolSearchCallParam
	OfToolSearchOutput   *ResponseToolSearchOutputItemParam
}

func (u ResponseInputItemUnionParam) IsZero() bool {
	return u.OfMessage == nil && u.OfInputMessage == nil && u.OfFunctionCall == nil &&
		u.OfFunctionCallOutput == nil && u.OfToolSearchCall == nil && u.OfToolSearchOutput == nil
}

func (u ResponseInputItemUnionParam) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfMessage != nil:
		return encjson.Marshal(u.OfMessage)
	case u.OfInputMessage != nil:
		return encjson.Marshal(u.OfInputMessage)
	case u.OfFunctionCall != nil:
		return encjson.Marshal(u.OfFunctionCall)
	case u.OfFunctionCallOutput != nil:
		return encjson.Marshal(u.OfFunctionCallOutput)
	case u.OfToolSearchCall != nil:
		return encjson.Marshal(u.OfToolSearchCall)
	case u.OfToolSearchOutput != nil:
		return encjson.Marshal(u.OfToolSearchOutput)
	}
	return []byte("null"), nil
}

// EasyInputMessageRole is the role of an [EasyInputMessageParam].
type EasyInputMessageRole string

const (
	EasyInputMessageRoleUser      EasyInputMessageRole = "user"
	EasyInputMessageRoleAssistant EasyInputMessageRole = "assistant"
	EasyInputMessageRoleSystem    EasyInputMessageRole = "system"
	EasyInputMessageRoleDeveloper EasyInputMessageRole = "developer"
)

// EasyInputMessageParam is a message whose content may be a plain string.
type EasyInputMessageParam struct {
	Content EasyInputMessageContentUnionParam `json:"content,omitzero"`
	Role    EasyInputMessageRole              `json:"role,omitzero"`
	Type    string                            `json:"type,omitzero"`
}

// EasyInputMessageContentUnionParam is a string or a content list.
type EasyInputMessageContentUnionParam struct {
	OfString               param.Opt[string]
	OfInputItemContentList ResponseInputMessageContentListParam
}

func (u EasyInputMessageContentUnionParam) IsZero() bool {
	return u.OfString.IsZero() && u.OfInputItemContentList == nil
}

func (u EasyInputMessageContentUnionParam) MarshalJSON() ([]byte, error) {
	if u.OfString.Valid() {
		return encjson.Marshal(u.OfString)
	}
	if u.OfInputItemContentList != nil {
		return encjson.Marshal(u.OfInputItemContentList)
	}
	return []byte("null"), nil
}

// ResponseInputItemMessageParam is a message whose content is always a list of
// typed blocks.
type ResponseInputItemMessageParam struct {
	Content ResponseInputMessageContentListParam `json:"content,omitzero"`
	Role    string                               `json:"role,omitzero"`
	Status  string                               `json:"status,omitzero"`
	Type    string                               `json:"type,omitzero"`
}

// ResponseInputMessageContentListParam is a list of message content blocks.
type ResponseInputMessageContentListParam []ResponseInputContentUnionParam

// ResponseInputContentUnionParam is one content block of a message.
type ResponseInputContentUnionParam struct {
	OfInputText  *ResponseInputTextParam
	OfInputImage *ResponseInputImageParam
	OfInputFile  *ResponseInputFileParam
}

func (u ResponseInputContentUnionParam) IsZero() bool {
	return u.OfInputText == nil && u.OfInputImage == nil && u.OfInputFile == nil
}

func (u ResponseInputContentUnionParam) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfInputText != nil:
		return encjson.Marshal(u.OfInputText)
	case u.OfInputImage != nil:
		return encjson.Marshal(u.OfInputImage)
	case u.OfInputFile != nil:
		return encjson.Marshal(u.OfInputFile)
	}
	return []byte("null"), nil
}

// ResponseInputTextParam is a text block of a message.
type ResponseInputTextParam struct {
	Text                  string                                      `json:"text"`
	PromptCacheBreakpoint ResponseInputTextPromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.InputText                          `json:"type"`
}

// ResponseInputImageDetail is the requested image fidelity.
type ResponseInputImageDetail string

// ResponseInputImageContentDetail is the same enum on the content-item form.
type ResponseInputImageContentDetail string

const (
	ResponseInputImageContentDetailLow  ResponseInputImageContentDetail = "low"
	ResponseInputImageContentDetailHigh ResponseInputImageContentDetail = "high"
	ResponseInputImageContentDetailAuto ResponseInputImageContentDetail = "auto"

	ResponseInputImageDetailLow  ResponseInputImageDetail = "low"
	ResponseInputImageDetailHigh ResponseInputImageDetail = "high"
	ResponseInputImageDetailAuto ResponseInputImageDetail = "auto"
)

// ResponseInputImageParam is an image block of a message.
type ResponseInputImageParam struct {
	Detail                ResponseInputImageDetail                     `json:"detail,omitzero"`
	FileID                param.Opt[string]                            `json:"file_id,omitzero"`
	ImageURL              param.Opt[string]                            `json:"image_url,omitzero"`
	PromptCacheBreakpoint ResponseInputImagePromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.InputImage                          `json:"type"`
}

// ResponseInputFileParam is a file block of a message.
type ResponseInputFileParam struct {
	FileID                param.Opt[string]                           `json:"file_id,omitzero"`
	FileData              param.Opt[string]                           `json:"file_data,omitzero"`
	FileURL               param.Opt[string]                           `json:"file_url,omitzero"`
	Filename              param.Opt[string]                           `json:"filename,omitzero"`
	Detail                string                                      `json:"detail,omitzero"`
	PromptCacheBreakpoint ResponseInputFilePromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.InputFile                          `json:"type"`
}

// ResponseFunctionToolCallParam replays a tool call made by the model.
type ResponseFunctionToolCallParam struct {
	Arguments string                `json:"arguments"`
	CallID    string                `json:"call_id"`
	Name      string                `json:"name"`
	ID        param.Opt[string]     `json:"id,omitzero"`
	Status    string                `json:"status,omitzero"`
	Type      constant.FunctionCall `json:"type"`
}

// ResponseInputItemFunctionCallOutputParam carries a tool result.
type ResponseInputItemFunctionCallOutputParam struct {
	CallID string                                              `json:"call_id"`
	Output ResponseInputItemFunctionCallOutputOutputUnionParam `json:"output,omitzero"`
	ID     param.Opt[string]                                   `json:"id,omitzero"`
	Status string                                              `json:"status,omitzero"`
	Type   constant.FunctionCallOutput                         `json:"type"`
}

// ResponseInputItemFunctionCallOutputOutputUnionParam is a tool result: plain
// text, or typed content blocks.
type ResponseInputItemFunctionCallOutputOutputUnionParam struct {
	OfString                              param.Opt[string]
	OfResponseFunctionCallOutputItemArray ResponseFunctionCallOutputItemListParam
}

func (u ResponseInputItemFunctionCallOutputOutputUnionParam) IsZero() bool {
	return u.OfString.IsZero() && u.OfResponseFunctionCallOutputItemArray == nil
}

func (u ResponseInputItemFunctionCallOutputOutputUnionParam) MarshalJSON() ([]byte, error) {
	if u.OfString.Valid() {
		return encjson.Marshal(u.OfString)
	}
	if u.OfResponseFunctionCallOutputItemArray != nil {
		return encjson.Marshal(u.OfResponseFunctionCallOutputItemArray)
	}
	return []byte("null"), nil
}

// ResponseFunctionCallOutputItemListParam is a list of tool-result blocks.
type ResponseFunctionCallOutputItemListParam []ResponseFunctionCallOutputItemUnionParam

// ResponseFunctionCallOutputItemUnionParam is one tool-result block.
type ResponseFunctionCallOutputItemUnionParam struct {
	OfInputText  *ResponseInputTextContentParam
	OfInputImage *ResponseInputImageContentParam
	OfInputFile  *ResponseInputFileContentParam
}

func (u ResponseFunctionCallOutputItemUnionParam) IsZero() bool {
	return u.OfInputText == nil && u.OfInputImage == nil && u.OfInputFile == nil
}

func (u ResponseFunctionCallOutputItemUnionParam) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfInputText != nil:
		return encjson.Marshal(u.OfInputText)
	case u.OfInputImage != nil:
		return encjson.Marshal(u.OfInputImage)
	case u.OfInputFile != nil:
		return encjson.Marshal(u.OfInputFile)
	}
	return []byte("null"), nil
}

// ResponseInputTextContentParam is a text block of a tool result.
type ResponseInputTextContentParam struct {
	Text                  string                                             `json:"text"`
	PromptCacheBreakpoint ResponseInputTextContentPromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.InputText                                 `json:"type"`
}

// ResponseInputImageContentParam is an image block of a tool result.
type ResponseInputImageContentParam struct {
	Detail                ResponseInputImageContentDetail                     `json:"detail,omitzero"`
	FileID                param.Opt[string]                                   `json:"file_id,omitzero"`
	ImageURL              param.Opt[string]                                   `json:"image_url,omitzero"`
	PromptCacheBreakpoint ResponseInputImageContentPromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.InputImage                                 `json:"type"`
}

// ResponseInputFileContentParam is a file block of a tool result.
type ResponseInputFileContentParam struct {
	FileID                param.Opt[string]                                  `json:"file_id,omitzero"`
	FileData              param.Opt[string]                                  `json:"file_data,omitzero"`
	FileURL               param.Opt[string]                                  `json:"file_url,omitzero"`
	Filename              param.Opt[string]                                  `json:"filename,omitzero"`
	PromptCacheBreakpoint ResponseInputFileContentPromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.InputFile                                 `json:"type"`
}

// The prompt-cache breakpoint marks the end of a reusable prompt prefix.
type (
	ResponseInputTextPromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ResponseInputImagePromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ResponseInputFilePromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ResponseInputTextContentPromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ResponseInputImageContentPromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ResponseInputFileContentPromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
)

func NewResponseInputTextPromptCacheBreakpointParam() ResponseInputTextPromptCacheBreakpointParam {
	return ResponseInputTextPromptCacheBreakpointParam{Mode: "explicit"}
}

func NewResponseInputImagePromptCacheBreakpointParam() ResponseInputImagePromptCacheBreakpointParam {
	return ResponseInputImagePromptCacheBreakpointParam{Mode: "explicit"}
}

func NewResponseInputFilePromptCacheBreakpointParam() ResponseInputFilePromptCacheBreakpointParam {
	return ResponseInputFilePromptCacheBreakpointParam{Mode: "explicit"}
}

func NewResponseInputTextContentPromptCacheBreakpointParam() ResponseInputTextContentPromptCacheBreakpointParam {
	return ResponseInputTextContentPromptCacheBreakpointParam{Mode: "explicit"}
}

func NewResponseInputImageContentPromptCacheBreakpointParam() ResponseInputImageContentPromptCacheBreakpointParam {
	return ResponseInputImageContentPromptCacheBreakpointParam{Mode: "explicit"}
}

func NewResponseInputFileContentPromptCacheBreakpointParam() ResponseInputFileContentPromptCacheBreakpointParam {
	return ResponseInputFileContentPromptCacheBreakpointParam{Mode: "explicit"}
}

// --- tool search (deferred tool loading) ----------------------------------

type (
	ResponseToolSearchOutputItemParamStatus    string
	ResponseToolSearchOutputItemParamExecution string
)

const (
	ResponseToolSearchOutputItemParamStatusInProgress ResponseToolSearchOutputItemParamStatus = "in_progress"
	ResponseToolSearchOutputItemParamStatusCompleted  ResponseToolSearchOutputItemParamStatus = "completed"

	ResponseToolSearchOutputItemParamExecutionClient ResponseToolSearchOutputItemParamExecution = "client"
	ResponseToolSearchOutputItemParamExecutionServer ResponseToolSearchOutputItemParamExecution = "server"
)

// ResponseInputItemToolSearchCallParam replays a tool-search call.
type ResponseInputItemToolSearchCallParam struct {
	Arguments any                     `json:"arguments,omitzero"`
	ID        param.Opt[string]       `json:"id,omitzero"`
	CallID    param.Opt[string]       `json:"call_id,omitzero"`
	Status    string                  `json:"status,omitzero"`
	Execution string                  `json:"execution,omitzero"`
	Type      constant.ToolSearchCall `json:"type"`
}

// ResponseToolSearchOutputItemParam carries the tools a tool-search returned.
type ResponseToolSearchOutputItemParam struct {
	Tools     []ToolUnionParam                           `json:"tools,omitzero"`
	ID        param.Opt[string]                          `json:"id,omitzero"`
	CallID    param.Opt[string]                          `json:"call_id,omitzero"`
	Status    ResponseToolSearchOutputItemParamStatus    `json:"status,omitzero"`
	Execution ResponseToolSearchOutputItemParamExecution `json:"execution,omitzero"`
	Type      constant.ToolSearchOutput                  `json:"type"`
}
