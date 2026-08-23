package openai

import (
	"context"
	"net/http"

	"github.com/openai/openai-go/v3/internal/apijson"
	"github.com/openai/openai-go/v3/internal/encjson"
	"github.com/openai/openai-go/v3/internal/requestconfig"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
)

// --- services -------------------------------------------------------------

// ChatService groups the chat endpoints.
type ChatService struct {
	Options     []option.RequestOption
	Completions ChatCompletionService
}

func NewChatService(opts ...option.RequestOption) ChatService {
	return ChatService{Options: opts, Completions: ChatCompletionService{Options: opts}}
}

// ChatCompletionService talks to POST /chat/completions.
type ChatCompletionService struct {
	Options []option.RequestOption
}

// New creates a chat completion and waits for the full response.
func (r ChatCompletionService) New(ctx context.Context, body ChatCompletionNewParams, opts ...option.RequestOption) (*ChatCompletion, error) {
	encoded, err := encjson.Marshal(body)
	if err != nil {
		return nil, err
	}
	res := &ChatCompletion{}
	err = requestconfig.ExecuteNewRequest(ctx, http.MethodPost, "chat/completions", encoded, res, append(r.Options[:len(r.Options):len(r.Options)], opts...)...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// NewStreaming creates a chat completion streamed as server-sent events. A
// request failure is carried by the returned stream's Err.
func (r ChatCompletionService) NewStreaming(ctx context.Context, body ChatCompletionNewParams, opts ...option.RequestOption) *ssestream.Stream[ChatCompletionChunk] {
	var raw *http.Response
	encoded, err := marshalStreaming(body)
	if err == nil {
		err = requestconfig.ExecuteNewRequest(ctx, http.MethodPost, "chat/completions", encoded, &raw,
			append(r.Options[:len(r.Options):len(r.Options)], opts...)...)
	}
	return ssestream.NewStream[ChatCompletionChunk](ssestream.NewDecoder(raw), err)
}

// marshalStreaming encodes a request body with `stream: true` added.
func marshalStreaming(body any) ([]byte, error) {
	encoded, err := encjson.Marshal(body)
	if err != nil {
		return nil, err
	}
	return apijson.MergeFields(encoded, map[string]any{"stream": true})
}

// --- request params -------------------------------------------------------

// ChatCompletionNewParams is the body of a chat completion request.
type ChatCompletionNewParams struct {
	Messages            []ChatCompletionMessageParamUnion          `json:"messages,omitzero"`
	Model               shared.ChatModel                           `json:"model,omitzero"`
	FrequencyPenalty    param.Opt[float64]                         `json:"frequency_penalty,omitzero"`
	MaxCompletionTokens param.Opt[int64]                           `json:"max_completion_tokens,omitzero"`
	MaxTokens           param.Opt[int64]                           `json:"max_tokens,omitzero"`
	N                   param.Opt[int64]                           `json:"n,omitzero"`
	PresencePenalty     param.Opt[float64]                         `json:"presence_penalty,omitzero"`
	PromptCacheKey      param.Opt[string]                          `json:"prompt_cache_key,omitzero"`
	Seed                param.Opt[int64]                           `json:"seed,omitzero"`
	Store               param.Opt[bool]                            `json:"store,omitzero"`
	Temperature         param.Opt[float64]                         `json:"temperature,omitzero"`
	TopP                param.Opt[float64]                         `json:"top_p,omitzero"`
	ParallelToolCalls   param.Opt[bool]                            `json:"parallel_tool_calls,omitzero"`
	User                param.Opt[string]                          `json:"user,omitzero"`
	LogitBias           map[string]int64                           `json:"logit_bias,omitzero"`
	Metadata            shared.Metadata                            `json:"metadata,omitzero"`
	ReasoningEffort     shared.ReasoningEffort                     `json:"reasoning_effort,omitzero"`
	Stop                []string                                   `json:"stop,omitzero"`
	StreamOptions       ChatCompletionStreamOptionsParam           `json:"stream_options,omitzero"`
	ResponseFormat      ChatCompletionNewParamsResponseFormatUnion `json:"response_format,omitzero"`
	ToolChoice          ChatCompletionToolChoiceOptionUnionParam   `json:"tool_choice,omitzero"`
	Tools               []ChatCompletionToolUnionParam             `json:"tools,omitzero"`

	extraFields map[string]any
}

// SetExtraFields adds body fields the typed struct doesn't model, for
// OpenAI-compatible backends that accept extra sampling parameters.
func (p *ChatCompletionNewParams) SetExtraFields(fields map[string]any) { p.extraFields = fields }

func (p ChatCompletionNewParams) MarshalJSON() ([]byte, error) {
	type alias ChatCompletionNewParams
	encoded, err := encjson.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return apijson.MergeFields(encoded, p.extraFields)
}

// ChatCompletionStreamOptionsParam configures what a stream reports.
type ChatCompletionStreamOptionsParam struct {
	IncludeObfuscation param.Opt[bool] `json:"include_obfuscation,omitzero"`
	IncludeUsage       param.Opt[bool] `json:"include_usage,omitzero"`
}

// ChatCompletionNewParamsResponseFormatUnion selects the output format.
type ChatCompletionNewParamsResponseFormatUnion struct {
	OfText       *shared.ResponseFormatTextParam
	OfJSONSchema *shared.ResponseFormatJSONSchemaParam
	OfJSONObject *shared.ResponseFormatJSONObjectParam
}

func (u ChatCompletionNewParamsResponseFormatUnion) IsZero() bool {
	return u.OfText == nil && u.OfJSONSchema == nil && u.OfJSONObject == nil
}

func (u ChatCompletionNewParamsResponseFormatUnion) MarshalJSON() ([]byte, error) {
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

// ChatCompletionToolChoiceOptionUnionParam is either a mode ("auto", "none",
// "required") or a named tool.
type ChatCompletionToolChoiceOptionUnionParam struct {
	OfAuto               param.Opt[string]
	OfFunctionToolChoice *ChatCompletionNamedToolChoiceParam
}

func (u ChatCompletionToolChoiceOptionUnionParam) IsZero() bool {
	return u.OfAuto.IsZero() && u.OfFunctionToolChoice == nil
}

func (u ChatCompletionToolChoiceOptionUnionParam) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfAuto.Valid():
		return encjson.Marshal(u.OfAuto)
	case u.OfFunctionToolChoice != nil:
		return encjson.Marshal(u.OfFunctionToolChoice)
	}
	return []byte("null"), nil
}

// ChatCompletionNamedToolChoiceParam forces one function.
type ChatCompletionNamedToolChoiceParam struct {
	Function ChatCompletionNamedToolChoiceFunctionParam `json:"function,omitzero"`
	Type     constant.Function                          `json:"type"`
}

type ChatCompletionNamedToolChoiceFunctionParam struct {
	Name string `json:"name"`
}

// ChatCompletionToolUnionParam is one tool exposed to the model.
type ChatCompletionToolUnionParam struct {
	OfFunction *ChatCompletionFunctionToolParam
}

func (u ChatCompletionToolUnionParam) IsZero() bool { return u.OfFunction == nil }

func (u ChatCompletionToolUnionParam) MarshalJSON() ([]byte, error) {
	if u.OfFunction != nil {
		return encjson.Marshal(u.OfFunction)
	}
	return []byte("null"), nil
}

// ChatCompletionFunctionTool wraps a function definition as a tool.
func ChatCompletionFunctionTool(function shared.FunctionDefinitionParam) ChatCompletionToolUnionParam {
	return ChatCompletionToolUnionParam{OfFunction: &ChatCompletionFunctionToolParam{Function: function}}
}

type ChatCompletionFunctionToolParam struct {
	Function shared.FunctionDefinitionParam `json:"function,omitzero"`
	Type     constant.Function              `json:"type"`
}

// --- messages -------------------------------------------------------------

// ChatCompletionMessageParamUnion is one message of the conversation; exactly
// one variant is set.
type ChatCompletionMessageParamUnion struct {
	OfSystem    *ChatCompletionSystemMessageParam
	OfUser      *ChatCompletionUserMessageParam
	OfAssistant *ChatCompletionAssistantMessageParam
	OfTool      *ChatCompletionToolMessageParam
}

func (u ChatCompletionMessageParamUnion) IsZero() bool {
	return u.OfSystem == nil && u.OfUser == nil && u.OfAssistant == nil && u.OfTool == nil
}

func (u ChatCompletionMessageParamUnion) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfSystem != nil:
		return encjson.Marshal(u.OfSystem)
	case u.OfUser != nil:
		return encjson.Marshal(u.OfUser)
	case u.OfAssistant != nil:
		return encjson.Marshal(u.OfAssistant)
	case u.OfTool != nil:
		return encjson.Marshal(u.OfTool)
	}
	return []byte("null"), nil
}

// SystemMessage builds a system message from a string or from text parts.
func SystemMessage[T string | []ChatCompletionContentPartTextParam](content T) ChatCompletionMessageParamUnion {
	var system ChatCompletionSystemMessageParam
	switch v := any(content).(type) {
	case string:
		system.Content.OfString = param.NewOpt(v)
	case []ChatCompletionContentPartTextParam:
		system.Content.OfArrayOfContentParts = v
	}
	return ChatCompletionMessageParamUnion{OfSystem: &system}
}

// UserMessage builds a user message from a string or from content parts.
func UserMessage[T string | []ChatCompletionContentPartUnionParam](content T) ChatCompletionMessageParamUnion {
	var user ChatCompletionUserMessageParam
	switch v := any(content).(type) {
	case string:
		user.Content.OfString = param.NewOpt(v)
	case []ChatCompletionContentPartUnionParam:
		user.Content.OfArrayOfContentParts = v
	}
	return ChatCompletionMessageParamUnion{OfUser: &user}
}

// AssistantMessage builds an assistant message from a string or content parts.
func AssistantMessage[T string | []ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion](content T) ChatCompletionMessageParamUnion {
	var assistant ChatCompletionAssistantMessageParam
	switch v := any(content).(type) {
	case string:
		assistant.Content.OfString = param.NewOpt(v)
	case []ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion:
		assistant.Content.OfArrayOfContentParts = v
	}
	return ChatCompletionMessageParamUnion{OfAssistant: &assistant}
}

// ToolMessage builds a tool result message.
func ToolMessage[T string | []ChatCompletionContentPartTextParam](content T, toolCallID string) ChatCompletionMessageParamUnion {
	tool := ChatCompletionToolMessageParam{ToolCallID: toolCallID}
	switch v := any(content).(type) {
	case string:
		tool.Content.OfString = param.NewOpt(v)
	case []ChatCompletionContentPartTextParam:
		tool.Content.OfArrayOfContentParts = v
	}
	return ChatCompletionMessageParamUnion{OfTool: &tool}
}

type ChatCompletionSystemMessageParam struct {
	Content ChatCompletionSystemMessageParamContentUnion `json:"content,omitzero"`
	Name    param.Opt[string]                            `json:"name,omitzero"`
	Role    constant.System                              `json:"role"`
}

type ChatCompletionSystemMessageParamContentUnion struct {
	OfString              param.Opt[string]
	OfArrayOfContentParts []ChatCompletionContentPartTextParam
}

func (u ChatCompletionSystemMessageParamContentUnion) IsZero() bool {
	return u.OfString.IsZero() && u.OfArrayOfContentParts == nil
}

func (u ChatCompletionSystemMessageParamContentUnion) MarshalJSON() ([]byte, error) {
	return marshalStringOrParts(u.OfString, u.OfArrayOfContentParts)
}

type ChatCompletionUserMessageParam struct {
	Content ChatCompletionUserMessageParamContentUnion `json:"content,omitzero"`
	Name    param.Opt[string]                          `json:"name,omitzero"`
	Role    constant.User                              `json:"role"`
}

type ChatCompletionUserMessageParamContentUnion struct {
	OfString              param.Opt[string]
	OfArrayOfContentParts []ChatCompletionContentPartUnionParam
}

func (u ChatCompletionUserMessageParamContentUnion) IsZero() bool {
	return u.OfString.IsZero() && u.OfArrayOfContentParts == nil
}

func (u ChatCompletionUserMessageParamContentUnion) MarshalJSON() ([]byte, error) {
	return marshalStringOrParts(u.OfString, u.OfArrayOfContentParts)
}

type ChatCompletionAssistantMessageParam struct {
	Refusal      param.Opt[string]                               `json:"refusal,omitzero"`
	Name         param.Opt[string]                               `json:"name,omitzero"`
	Content      ChatCompletionAssistantMessageParamContentUnion `json:"content,omitzero"`
	FunctionCall ChatCompletionAssistantMessageParamFunctionCall `json:"function_call,omitzero"`
	ToolCalls    []ChatCompletionMessageToolCallUnionParam       `json:"tool_calls,omitzero"`
	Role         constant.Assistant                              `json:"role"`
}

type ChatCompletionAssistantMessageParamFunctionCall struct {
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

type ChatCompletionAssistantMessageParamContentUnion struct {
	OfString              param.Opt[string]
	OfArrayOfContentParts []ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion
}

func (u ChatCompletionAssistantMessageParamContentUnion) IsZero() bool {
	return u.OfString.IsZero() && u.OfArrayOfContentParts == nil
}

func (u ChatCompletionAssistantMessageParamContentUnion) MarshalJSON() ([]byte, error) {
	return marshalStringOrParts(u.OfString, u.OfArrayOfContentParts)
}

// ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion is one
// block of an assistant message: text or a refusal.
type ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion struct {
	OfText    *ChatCompletionContentPartTextParam
	OfRefusal *ChatCompletionContentPartRefusalParam
}

func (u ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion) IsZero() bool {
	return u.OfText == nil && u.OfRefusal == nil
}

func (u ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfText != nil:
		return encjson.Marshal(u.OfText)
	case u.OfRefusal != nil:
		return encjson.Marshal(u.OfRefusal)
	}
	return []byte("null"), nil
}

type ChatCompletionContentPartRefusalParam struct {
	Refusal string `json:"refusal"`
	Type    string `json:"type"`
}

type ChatCompletionToolMessageParam struct {
	Content    ChatCompletionToolMessageParamContentUnion `json:"content,omitzero"`
	ToolCallID string                                     `json:"tool_call_id"`
	Role       constant.Tool                              `json:"role"`
}

type ChatCompletionToolMessageParamContentUnion struct {
	OfString              param.Opt[string]
	OfArrayOfContentParts []ChatCompletionContentPartTextParam
}

func (u ChatCompletionToolMessageParamContentUnion) IsZero() bool {
	return u.OfString.IsZero() && u.OfArrayOfContentParts == nil
}

func (u ChatCompletionToolMessageParamContentUnion) MarshalJSON() ([]byte, error) {
	return marshalStringOrParts(u.OfString, u.OfArrayOfContentParts)
}

// marshalStringOrParts serializes whichever side of a string-or-parts content
// union is set.
func marshalStringOrParts[T any](str param.Opt[string], parts []T) ([]byte, error) {
	if str.Valid() {
		return encjson.Marshal(str)
	}
	if parts != nil {
		return encjson.Marshal(parts)
	}
	return []byte("null"), nil
}

// --- content parts --------------------------------------------------------

// ChatCompletionContentPartUnionParam is one block of a user message.
type ChatCompletionContentPartUnionParam struct {
	OfText       *ChatCompletionContentPartTextParam
	OfImageURL   *ChatCompletionContentPartImageParam
	OfInputAudio *ChatCompletionContentPartInputAudioParam
	OfFile       *ChatCompletionContentPartFileParam
}

func (u ChatCompletionContentPartUnionParam) IsZero() bool {
	return u.OfText == nil && u.OfImageURL == nil && u.OfInputAudio == nil && u.OfFile == nil
}

func (u ChatCompletionContentPartUnionParam) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfText != nil:
		return encjson.Marshal(u.OfText)
	case u.OfImageURL != nil:
		return encjson.Marshal(u.OfImageURL)
	case u.OfInputAudio != nil:
		return encjson.Marshal(u.OfInputAudio)
	case u.OfFile != nil:
		return encjson.Marshal(u.OfFile)
	}
	return []byte("null"), nil
}

// TextContentPart, ImageContentPart, InputAudioContentPart and
// FileContentPart build the corresponding user-message blocks.
func TextContentPart(text string) ChatCompletionContentPartUnionParam {
	return ChatCompletionContentPartUnionParam{OfText: &ChatCompletionContentPartTextParam{Text: text}}
}

func ImageContentPart(imageURL ChatCompletionContentPartImageImageURLParam) ChatCompletionContentPartUnionParam {
	return ChatCompletionContentPartUnionParam{OfImageURL: &ChatCompletionContentPartImageParam{ImageURL: imageURL}}
}

func InputAudioContentPart(inputAudio ChatCompletionContentPartInputAudioInputAudioParam) ChatCompletionContentPartUnionParam {
	return ChatCompletionContentPartUnionParam{OfInputAudio: &ChatCompletionContentPartInputAudioParam{InputAudio: inputAudio}}
}

func FileContentPart(file ChatCompletionContentPartFileFileParam) ChatCompletionContentPartUnionParam {
	return ChatCompletionContentPartUnionParam{OfFile: &ChatCompletionContentPartFileParam{File: file}}
}

type ChatCompletionContentPartTextParam struct {
	Text                  string                                                  `json:"text"`
	PromptCacheBreakpoint ChatCompletionContentPartTextPromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.Text                                           `json:"type"`
}

type ChatCompletionContentPartImageParam struct {
	ImageURL              ChatCompletionContentPartImageImageURLParam              `json:"image_url,omitzero"`
	PromptCacheBreakpoint ChatCompletionContentPartImagePromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.ImageURL                                        `json:"type"`
}

type ChatCompletionContentPartImageImageURLParam struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitzero"`
}

type ChatCompletionContentPartInputAudioParam struct {
	InputAudio            ChatCompletionContentPartInputAudioInputAudioParam            `json:"input_audio,omitzero"`
	PromptCacheBreakpoint ChatCompletionContentPartInputAudioPromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.InputAudio                                           `json:"type"`
}

type ChatCompletionContentPartInputAudioInputAudioParam struct {
	Data   string `json:"data"`
	Format string `json:"format,omitzero"`
}

type ChatCompletionContentPartFileParam struct {
	File                  ChatCompletionContentPartFileFileParam                  `json:"file,omitzero"`
	PromptCacheBreakpoint ChatCompletionContentPartFilePromptCacheBreakpointParam `json:"prompt_cache_breakpoint,omitzero"`
	Type                  constant.File                                           `json:"type"`
}

type ChatCompletionContentPartFileFileParam struct {
	FileData param.Opt[string] `json:"file_data,omitzero"`
	FileID   param.Opt[string] `json:"file_id,omitzero"`
	Filename param.Opt[string] `json:"filename,omitzero"`
}

// The prompt-cache breakpoint marks the end of a reusable prompt prefix. Each
// content part type carries its own (identical) breakpoint type.
type (
	ChatCompletionContentPartTextPromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ChatCompletionContentPartImagePromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ChatCompletionContentPartInputAudioPromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
	ChatCompletionContentPartFilePromptCacheBreakpointParam struct {
		Mode constant.Explicit `json:"mode"`
	}
)

func NewChatCompletionContentPartTextPromptCacheBreakpointParam() ChatCompletionContentPartTextPromptCacheBreakpointParam {
	return ChatCompletionContentPartTextPromptCacheBreakpointParam{Mode: "explicit"}
}

func NewChatCompletionContentPartImagePromptCacheBreakpointParam() ChatCompletionContentPartImagePromptCacheBreakpointParam {
	return ChatCompletionContentPartImagePromptCacheBreakpointParam{Mode: "explicit"}
}

func NewChatCompletionContentPartInputAudioPromptCacheBreakpointParam() ChatCompletionContentPartInputAudioPromptCacheBreakpointParam {
	return ChatCompletionContentPartInputAudioPromptCacheBreakpointParam{Mode: "explicit"}
}

func NewChatCompletionContentPartFilePromptCacheBreakpointParam() ChatCompletionContentPartFilePromptCacheBreakpointParam {
	return ChatCompletionContentPartFilePromptCacheBreakpointParam{Mode: "explicit"}
}

// --- tool calls -----------------------------------------------------------

// ChatCompletionMessageToolCallUnionParam is a tool call replayed to the model.
type ChatCompletionMessageToolCallUnionParam struct {
	OfFunction *ChatCompletionMessageFunctionToolCallParam
}

func (u ChatCompletionMessageToolCallUnionParam) IsZero() bool { return u.OfFunction == nil }

func (u ChatCompletionMessageToolCallUnionParam) MarshalJSON() ([]byte, error) {
	if u.OfFunction != nil {
		return encjson.Marshal(u.OfFunction)
	}
	return []byte("null"), nil
}

type ChatCompletionMessageFunctionToolCallParam struct {
	ID       string                                             `json:"id"`
	Function ChatCompletionMessageFunctionToolCallFunctionParam `json:"function,omitzero"`
	Type     constant.Function                                  `json:"type"`
}

type ChatCompletionMessageFunctionToolCallFunctionParam struct {
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

// --- responses ------------------------------------------------------------

// ChatCompletion is a non-streamed completion.
type ChatCompletion struct {
	ID                string                  `json:"id"`
	Choices           []ChatCompletionChoice  `json:"choices"`
	Created           int64                   `json:"created"`
	Model             string                  `json:"model"`
	Object            constant.ChatCompletion `json:"object"`
	ServiceTier       string                  `json:"service_tier"`
	SystemFingerprint string                  `json:"system_fingerprint"`
	Usage             CompletionUsage         `json:"usage"`
}

type ChatCompletionChoice struct {
	FinishReason string                `json:"finish_reason"`
	Index        int64                 `json:"index"`
	Message      ChatCompletionMessage `json:"message"`
}

// ChatCompletionMessage is the assistant message of a completion.
type ChatCompletionMessage struct {
	Content      string                            `json:"content"`
	Refusal      string                            `json:"refusal"`
	Role         constant.Assistant                `json:"role"`
	FunctionCall ChatCompletionMessageFunctionCall `json:"function_call,omitzero"`
	ToolCalls    []ChatCompletionMessageToolCall   `json:"tool_calls,omitempty"`
}

type ChatCompletionMessageFunctionCall struct {
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

type ChatCompletionMessageToolCall struct {
	ID       string                                `json:"id"`
	Function ChatCompletionMessageToolCallFunction `json:"function"`
	Type     string                                `json:"type"`
}

type ChatCompletionMessageToolCallFunction struct {
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

// ChatCompletionChunk is one server-sent event of a streamed completion.
type ChatCompletionChunk struct {
	ID                string                       `json:"id"`
	Choices           []ChatCompletionChunkChoice  `json:"choices"`
	Created           int64                        `json:"created"`
	Model             string                       `json:"model"`
	Object            constant.ChatCompletionChunk `json:"object"`
	ServiceTier       string                       `json:"service_tier"`
	SystemFingerprint string                       `json:"system_fingerprint"`
	Usage             CompletionUsage              `json:"usage"`
	JSON              struct {
		ID                respjson.Field
		Choices           respjson.Field
		Created           respjson.Field
		Model             respjson.Field
		Object            respjson.Field
		ServiceTier       respjson.Field
		SystemFingerprint respjson.Field
		Usage             respjson.Field
		ExtraFields       map[string]respjson.Field
		Raw               string
	} `json:"-"`
}

func (r ChatCompletionChunk) RawJSON() string { return r.JSON.Raw }

func (r *ChatCompletionChunk) UnmarshalJSON(data []byte) error {
	return apijson.UnmarshalRoot(data, r)
}

type ChatCompletionChunkChoice struct {
	Delta        ChatCompletionChunkChoiceDelta `json:"delta"`
	FinishReason string                         `json:"finish_reason"`
	Index        int64                          `json:"index"`
}

// ChatCompletionChunkChoiceDelta carries the incremental content. Fields the
// API doesn't model (a provider's "reasoning_content", ...) stay reachable
// through JSON.ExtraFields.
type ChatCompletionChunkChoiceDelta struct {
	Content      string                                     `json:"content"`
	FunctionCall ChatCompletionChunkChoiceDeltaFunctionCall `json:"function_call"`
	Refusal      string                                     `json:"refusal"`
	Role         string                                     `json:"role"`
	ToolCalls    []ChatCompletionChunkChoiceDeltaToolCall   `json:"tool_calls"`
	JSON         struct {
		Content      respjson.Field
		FunctionCall respjson.Field
		Refusal      respjson.Field
		Role         respjson.Field
		ToolCalls    respjson.Field
		ExtraFields  map[string]respjson.Field
		Raw          string
	} `json:"-"`
}

func (r ChatCompletionChunkChoiceDelta) RawJSON() string { return r.JSON.Raw }

func (r *ChatCompletionChunkChoiceDelta) UnmarshalJSON(data []byte) error {
	return apijson.UnmarshalRoot(data, r)
}

type ChatCompletionChunkChoiceDeltaFunctionCall struct {
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

type ChatCompletionChunkChoiceDeltaToolCall struct {
	Index    int64                                          `json:"index"`
	ID       string                                         `json:"id"`
	Function ChatCompletionChunkChoiceDeltaToolCallFunction `json:"function"`
	Type     string                                         `json:"type"`
}

type ChatCompletionChunkChoiceDeltaToolCallFunction struct {
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

// CompletionUsage reports token counts. The details sub-objects are only
// meaningful when their JSON metadata says the provider sent them.
type CompletionUsage struct {
	CompletionTokens        int64                                  `json:"completion_tokens"`
	PromptTokens            int64                                  `json:"prompt_tokens"`
	TotalTokens             int64                                  `json:"total_tokens"`
	CompletionTokensDetails CompletionUsageCompletionTokensDetails `json:"completion_tokens_details"`
	PromptTokensDetails     CompletionUsagePromptTokensDetails     `json:"prompt_tokens_details"`
	JSON                    struct {
		CompletionTokens        respjson.Field
		PromptTokens            respjson.Field
		TotalTokens             respjson.Field
		CompletionTokensDetails respjson.Field
		PromptTokensDetails     respjson.Field
		ExtraFields             map[string]respjson.Field
		Raw                     string
	} `json:"-"`
}

func (r CompletionUsage) RawJSON() string { return r.JSON.Raw }

func (r *CompletionUsage) UnmarshalJSON(data []byte) error { return apijson.UnmarshalRoot(data, r) }

type CompletionUsageCompletionTokensDetails struct {
	AcceptedPredictionTokens int64 `json:"accepted_prediction_tokens"`
	AudioTokens              int64 `json:"audio_tokens"`
	ReasoningTokens          int64 `json:"reasoning_tokens"`
	RejectedPredictionTokens int64 `json:"rejected_prediction_tokens"`
	TextTokens               int64 `json:"text_tokens"`
}

type CompletionUsagePromptTokensDetails struct {
	AudioTokens      int64 `json:"audio_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
	ImageTokens      int64 `json:"image_tokens"`
	TextTokens       int64 `json:"text_tokens"`
}
