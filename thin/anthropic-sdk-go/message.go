package anthropic

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// Model is a model identifier, e.g. "claude-sonnet-4-6".
type Model = string

// AnthropicBeta is the value of an `anthropic-beta` header entry.
type AnthropicBeta = string

// StopReason is why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn                    StopReason = "end_turn"
	StopReasonMaxTokens                  StopReason = "max_tokens"
	StopReasonStopSequence               StopReason = "stop_sequence"
	StopReasonToolUse                    StopReason = "tool_use"
	StopReasonPauseTurn                  StopReason = "pause_turn"
	StopReasonRefusal                    StopReason = "refusal"
	StopReasonModelContextWindowExceeded StopReason = "model_context_window_exceeded"
)

// MessageParamRole is the role of an input message.
type MessageParamRole string

const (
	MessageParamRoleUser      MessageParamRole = "user"
	MessageParamRoleAssistant MessageParamRole = "assistant"
)

// CacheControlEphemeralTTL is the lifetime of a cache breakpoint ("5m", "1h").
type CacheControlEphemeralTTL string

const (
	CacheControlEphemeralTTL5m CacheControlEphemeralTTL = "5m"
	CacheControlEphemeralTTL1h CacheControlEphemeralTTL = "1h"
)

// Base64ImageSourceMediaType is the MIME type of an inline image.
type Base64ImageSourceMediaType string

const (
	Base64ImageSourceMediaTypeImageJPEG Base64ImageSourceMediaType = "image/jpeg"
	Base64ImageSourceMediaTypeImagePNG  Base64ImageSourceMediaType = "image/png"
	Base64ImageSourceMediaTypeImageGIF  Base64ImageSourceMediaType = "image/gif"
	Base64ImageSourceMediaTypeImageWebP Base64ImageSourceMediaType = "image/webp"
)

// ThinkingConfigEnabledDisplay controls how token-budget thinking is shown.
type ThinkingConfigEnabledDisplay string

// ThinkingConfigAdaptiveDisplay controls how adaptive thinking is shown.
type ThinkingConfigAdaptiveDisplay string

const (
	ThinkingConfigDisplaySummarized ThinkingConfigEnabledDisplay = "summarized"
	ThinkingConfigDisplayOmitted    ThinkingConfigEnabledDisplay = "omitted"
	ThinkingConfigDisplayDisplay    ThinkingConfigEnabledDisplay = "display"
)

// OutputConfigEffort is the reasoning-effort level of a request.
type OutputConfigEffort string

const (
	OutputConfigEffortLow    OutputConfigEffort = "low"
	OutputConfigEffortMedium OutputConfigEffort = "medium"
	OutputConfigEffortHigh   OutputConfigEffort = "high"
)

// ToolType is the kind of a custom tool.
type ToolType string

const ToolTypeCustom ToolType = "custom"

// CacheControlEphemeralParam marks a prompt-caching breakpoint. Construct it
// with [NewCacheControlEphemeralParam].
type CacheControlEphemeralParam struct {
	TTL  CacheControlEphemeralTTL `json:"ttl,omitzero"`
	Type constEphemeral           `json:"type"`
}

func NewCacheControlEphemeralParam() CacheControlEphemeralParam {
	return CacheControlEphemeralParam{Type: "ephemeral"}
}

// TextBlockParam is a text content block.
type TextBlockParam struct {
	Text         string                     `json:"text"`
	CacheControl CacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constText                  `json:"type"`
}

// Base64ImageSourceParam is an inline base64-encoded image.
type Base64ImageSourceParam struct {
	Data      string                     `json:"data"`
	MediaType Base64ImageSourceMediaType `json:"media_type,omitzero"`
	Type      constBase64                `json:"type"`
}

// URLImageSourceParam references an image by URL.
type URLImageSourceParam struct {
	URL  string   `json:"url"`
	Type constURL `json:"type"`
}

// ImageBlockParamSourceUnion is the source of an [ImageBlockParam].
type ImageBlockParamSourceUnion struct {
	OfBase64 *Base64ImageSourceParam `json:",omitzero,inline"`
	OfURL    *URLImageSourceParam    `json:",omitzero,inline"`

	paramUnion
}

func (u ImageBlockParamSourceUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfBase64, u.OfURL)
}

func (u ImageBlockParamSourceUnion) IsZero() bool { return u.OfBase64 == nil && u.OfURL == nil }

// ImageBlockParam is an image content block.
type ImageBlockParam struct {
	Source       ImageBlockParamSourceUnion `json:"source,omitzero"`
	CacheControl CacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constImage                 `json:"type"`
}

// Base64PDFSourceParam is an inline base64-encoded PDF.
type Base64PDFSourceParam struct {
	Data      string              `json:"data"`
	MediaType constApplicationPDF `json:"media_type"`
	Type      constBase64         `json:"type"`
}

// DocumentBlockParamSourceUnion is the source of a [DocumentBlockParam].
type DocumentBlockParamSourceUnion struct {
	OfBase64 *Base64PDFSourceParam `json:",omitzero,inline"`

	paramUnion
}

func (u DocumentBlockParamSourceUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfBase64)
}

func (u DocumentBlockParamSourceUnion) IsZero() bool { return u.OfBase64 == nil }

// DocumentBlockParam is a document (PDF) content block.
type DocumentBlockParam struct {
	Source       DocumentBlockParamSourceUnion `json:"source,omitzero"`
	Context      param.Opt[string]             `json:"context,omitzero"`
	Title        param.Opt[string]             `json:"title,omitzero"`
	CacheControl CacheControlEphemeralParam    `json:"cache_control,omitzero"`
	Type         constDocument                 `json:"type"`
}

// ThinkingBlockParam replays a previous extended-thinking block.
type ThinkingBlockParam struct {
	Signature string        `json:"signature"`
	Thinking  string        `json:"thinking"`
	Type      constThinking `json:"type"`
}

// RedactedThinkingBlockParam replays a redacted thinking block.
type RedactedThinkingBlockParam struct {
	Data string                `json:"data"`
	Type constRedactedThinking `json:"type"`
}

// ToolUseBlockParam replays a tool call made by the model.
type ToolUseBlockParam struct {
	ID           string                     `json:"id"`
	Input        any                        `json:"input,omitzero"`
	Name         string                     `json:"name"`
	CacheControl CacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constToolUse               `json:"type"`
}

// ToolReferenceBlockParam activates a deferred tool by name.
type ToolReferenceBlockParam struct {
	ToolName     string                     `json:"tool_name"`
	CacheControl CacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constToolReference         `json:"type"`
}

// ToolResultBlockParamContentUnion is one block of a tool result's content.
type ToolResultBlockParamContentUnion struct {
	OfText          *TextBlockParam          `json:",omitzero,inline"`
	OfImage         *ImageBlockParam         `json:",omitzero,inline"`
	OfDocument      *DocumentBlockParam      `json:",omitzero,inline"`
	OfToolReference *ToolReferenceBlockParam `json:",omitzero,inline"`

	paramUnion
}

func (u ToolResultBlockParamContentUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfText, u.OfImage, u.OfDocument, u.OfToolReference)
}

func (u ToolResultBlockParamContentUnion) IsZero() bool {
	return u.OfText == nil && u.OfImage == nil && u.OfDocument == nil && u.OfToolReference == nil
}

// ToolResultBlockParam returns the result of a tool call to the model.
type ToolResultBlockParam struct {
	ToolUseID    string                             `json:"tool_use_id"`
	IsError      param.Opt[bool]                    `json:"is_error,omitzero"`
	CacheControl CacheControlEphemeralParam         `json:"cache_control,omitzero"`
	Content      []ToolResultBlockParamContentUnion `json:"content,omitzero"`
	Type         constToolResult                    `json:"type"`
}

// ContentBlockParamUnion is one content block of an input message.
type ContentBlockParamUnion struct {
	OfText             *TextBlockParam             `json:",omitzero,inline"`
	OfImage            *ImageBlockParam            `json:",omitzero,inline"`
	OfDocument         *DocumentBlockParam         `json:",omitzero,inline"`
	OfThinking         *ThinkingBlockParam         `json:",omitzero,inline"`
	OfRedactedThinking *RedactedThinkingBlockParam `json:",omitzero,inline"`
	OfToolUse          *ToolUseBlockParam          `json:",omitzero,inline"`
	OfToolResult       *ToolResultBlockParam       `json:",omitzero,inline"`

	paramUnion
}

func (u ContentBlockParamUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfText, u.OfImage, u.OfDocument, u.OfThinking, u.OfRedactedThinking, u.OfToolUse, u.OfToolResult)
}

func (u ContentBlockParamUnion) IsZero() bool {
	return u.OfText == nil && u.OfImage == nil && u.OfDocument == nil && u.OfThinking == nil &&
		u.OfRedactedThinking == nil && u.OfToolUse == nil && u.OfToolResult == nil
}

// GetCacheControl returns a pointer to the block's cache-control field, or
// nil for block kinds that cannot carry a breakpoint (thinking blocks).
func (u ContentBlockParamUnion) GetCacheControl() *CacheControlEphemeralParam {
	switch {
	case u.OfText != nil:
		return &u.OfText.CacheControl
	case u.OfImage != nil:
		return &u.OfImage.CacheControl
	case u.OfDocument != nil:
		return &u.OfDocument.CacheControl
	case u.OfToolUse != nil:
		return &u.OfToolUse.CacheControl
	case u.OfToolResult != nil:
		return &u.OfToolResult.CacheControl
	}
	return nil
}

// MessageParam is one input message of a conversation.
type MessageParam struct {
	Content []ContentBlockParamUnion `json:"content,omitzero"`
	Role    MessageParamRole         `json:"role,omitzero"`
}

func NewUserMessage(blocks ...ContentBlockParamUnion) MessageParam {
	return MessageParam{Role: MessageParamRoleUser, Content: blocks}
}

func NewAssistantMessage(blocks ...ContentBlockParamUnion) MessageParam {
	return MessageParam{Role: MessageParamRoleAssistant, Content: blocks}
}

func NewTextBlock(text string) ContentBlockParamUnion {
	return ContentBlockParamUnion{OfText: &TextBlockParam{Text: text}}
}

func NewImageBlock[T Base64ImageSourceParam | URLImageSourceParam](source T) ContentBlockParamUnion {
	var image ImageBlockParam
	switch v := any(source).(type) {
	case Base64ImageSourceParam:
		image.Source.OfBase64 = &v
	case URLImageSourceParam:
		image.Source.OfURL = &v
	}
	return ContentBlockParamUnion{OfImage: &image}
}

func NewDocumentBlock[T Base64PDFSourceParam](source T) ContentBlockParamUnion {
	var document DocumentBlockParam
	if v, ok := any(source).(Base64PDFSourceParam); ok {
		document.Source.OfBase64 = &v
	}
	return ContentBlockParamUnion{OfDocument: &document}
}

func NewThinkingBlock(signature, thinking string) ContentBlockParamUnion {
	return ContentBlockParamUnion{OfThinking: &ThinkingBlockParam{Signature: signature, Thinking: thinking}}
}

func NewRedactedThinkingBlock(data string) ContentBlockParamUnion {
	return ContentBlockParamUnion{OfRedactedThinking: &RedactedThinkingBlockParam{Data: data}}
}

func NewToolUseBlock(id string, input any, name string) ContentBlockParamUnion {
	return ContentBlockParamUnion{OfToolUse: &ToolUseBlockParam{ID: id, Input: input, Name: name}}
}

func NewToolResultBlock(toolUseID, content string, isError bool) ContentBlockParamUnion {
	return ContentBlockParamUnion{OfToolResult: &ToolResultBlockParam{
		ToolUseID: toolUseID,
		Content:   []ToolResultBlockParamContentUnion{{OfText: &TextBlockParam{Text: content}}},
		IsError:   Bool(isError),
	}}
}

// ToolInputSchemaParam is the JSON Schema of a tool's input.
type ToolInputSchemaParam struct {
	Properties  any            `json:"properties,omitzero"`
	Required    []string       `json:"required,omitzero"`
	Type        constObject    `json:"type"`
	ExtraFields map[string]any `json:"-"`
}

func (r ToolInputSchemaParam) MarshalJSON() ([]byte, error) {
	type shadow ToolInputSchemaParam
	return marshalWithExtras(shadow(r), r.ExtraFields)
}

// ToolParam is a custom tool definition.
type ToolParam struct {
	InputSchema  ToolInputSchemaParam       `json:"input_schema,omitzero"`
	Name         string                     `json:"name"`
	DeferLoading param.Opt[bool]            `json:"defer_loading,omitzero"`
	Description  param.Opt[string]          `json:"description,omitzero"`
	Strict       param.Opt[bool]            `json:"strict,omitzero"`
	Type         ToolType                   `json:"type,omitzero"`
	CacheControl CacheControlEphemeralParam `json:"cache_control,omitzero"`
}

// ToolUnionParam is one entry of a request's `tools` array.
type ToolUnionParam struct {
	OfTool *ToolParam `json:",omitzero,inline"`

	paramUnion
}

func (u ToolUnionParam) MarshalJSON() ([]byte, error) { return marshalUnion(u.OfTool) }
func (u ToolUnionParam) IsZero() bool                 { return u.OfTool == nil }

// MessageCountTokensToolUnionParam is one entry of a count-tokens `tools` array.
type MessageCountTokensToolUnionParam struct {
	OfTool *ToolParam `json:",omitzero,inline"`

	paramUnion
}

func (u MessageCountTokensToolUnionParam) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfTool)
}
func (u MessageCountTokensToolUnionParam) IsZero() bool { return u.OfTool == nil }

// ThinkingConfigEnabledParam requests token-budget extended thinking.
type ThinkingConfigEnabledParam struct {
	BudgetTokens int64                        `json:"budget_tokens"`
	Display      ThinkingConfigEnabledDisplay `json:"display,omitzero"`
	Type         constEnabled                 `json:"type"`
}

// ThinkingConfigAdaptiveParam requests adaptive (model-managed) thinking.
type ThinkingConfigAdaptiveParam struct {
	Display ThinkingConfigAdaptiveDisplay `json:"display,omitzero"`
	Type    constAdaptive                 `json:"type"`
}

// ThinkingConfigDisabledParam disables extended thinking.
type ThinkingConfigDisabledParam struct {
	Type constDisabled `json:"type"`
}

// ThinkingConfigParamUnion is the request's `thinking` field.
type ThinkingConfigParamUnion struct {
	OfEnabled  *ThinkingConfigEnabledParam  `json:",omitzero,inline"`
	OfDisabled *ThinkingConfigDisabledParam `json:",omitzero,inline"`
	OfAdaptive *ThinkingConfigAdaptiveParam `json:",omitzero,inline"`

	paramUnion
}

func (u ThinkingConfigParamUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfEnabled, u.OfDisabled, u.OfAdaptive)
}

func (u ThinkingConfigParamUnion) IsZero() bool {
	return u.OfEnabled == nil && u.OfDisabled == nil && u.OfAdaptive == nil
}

func ThinkingConfigParamOfEnabled(budgetTokens int64) ThinkingConfigParamUnion {
	return ThinkingConfigParamUnion{OfEnabled: &ThinkingConfigEnabledParam{BudgetTokens: budgetTokens}}
}

// JSONOutputFormatParam constrains the response to a JSON schema.
type JSONOutputFormatParam struct {
	Schema any             `json:"schema,omitzero"`
	Type   constJSONSchema `json:"type"`
}

// OutputConfigParam is the request's `output_config` field.
type OutputConfigParam struct {
	Effort OutputConfigEffort    `json:"effort,omitzero"`
	Format JSONOutputFormatParam `json:"format,omitzero"`

	extras map[string]any
}

// SetExtraFields adds fields to the serialized object, overriding any
// existing key. Only use it with trusted input.
func (r *OutputConfigParam) SetExtraFields(extraFields map[string]any) { r.extras = extraFields }

func (r OutputConfigParam) MarshalJSON() ([]byte, error) {
	type shadow OutputConfigParam
	return marshalWithExtras(shadow(r), r.extras)
}

func (r OutputConfigParam) IsZero() bool {
	return r.Effort == "" && r.Format.Schema == nil && len(r.extras) == 0
}

// MessageCountTokensParamsSystemUnion is the count-tokens `system` field.
type MessageCountTokensParamsSystemUnion struct {
	OfString         param.Opt[string] `json:",omitzero,inline"`
	OfTextBlockArray []TextBlockParam  `json:",omitzero,inline"`

	paramUnion
}

func (u MessageCountTokensParamsSystemUnion) MarshalJSON() ([]byte, error) {
	if u.OfString.Valid() {
		return json.Marshal(u.OfString)
	}
	return json.Marshal(u.OfTextBlockArray)
}

func (u MessageCountTokensParamsSystemUnion) IsZero() bool {
	return !u.OfString.Valid() && u.OfTextBlockArray == nil
}

// MessageNewParams is the body of a POST /v1/messages request.
type MessageNewParams struct {
	MaxTokens     int64                      `json:"max_tokens"`
	Messages      []MessageParam             `json:"messages,omitzero"`
	Model         Model                      `json:"model,omitzero"`
	Temperature   param.Opt[float64]         `json:"temperature,omitzero"`
	TopK          param.Opt[int64]           `json:"top_k,omitzero"`
	TopP          param.Opt[float64]         `json:"top_p,omitzero"`
	CacheControl  CacheControlEphemeralParam `json:"cache_control,omitzero"`
	OutputConfig  OutputConfigParam          `json:"output_config,omitzero"`
	StopSequences []string                   `json:"stop_sequences,omitzero"`
	System        []TextBlockParam           `json:"system,omitzero"`
	Thinking      ThinkingConfigParamUnion   `json:"thinking,omitzero"`
	Tools         []ToolUnionParam           `json:"tools,omitzero"`
}

// MessageCountTokensParams is the body of a count_tokens request.
type MessageCountTokensParams struct {
	Messages     []MessageParam                      `json:"messages,omitzero"`
	Model        Model                               `json:"model,omitzero"`
	OutputConfig OutputConfigParam                   `json:"output_config,omitzero"`
	System       MessageCountTokensParamsSystemUnion `json:"system,omitzero"`
	Thinking     ThinkingConfigParamUnion            `json:"thinking,omitzero"`
	Tools        []MessageCountTokensToolUnionParam  `json:"tools,omitzero"`
}

// MessageTokensCount is the response of a count_tokens request.
type MessageTokensCount struct {
	InputTokens int64 `json:"input_tokens"`
}

// NewStreaming posts a message and returns the SSE event stream.
func (r *MessageService) NewStreaming(ctx context.Context, params MessageNewParams, opts ...option.RequestOption) *ssestream.Stream[MessageStreamEventUnion] {
	var raw *http.Response
	opts = append(opts, option.WithJSONSet("stream", true))
	err := execute(ctx, "v1/messages", params, &raw, r.Options, opts)
	return ssestream.NewStream[MessageStreamEventUnion](ssestream.NewDecoder(raw), err)
}

// CountTokens counts the tokens a message would consume.
func (r *MessageService) CountTokens(ctx context.Context, params MessageCountTokensParams, opts ...option.RequestOption) (*MessageTokensCount, error) {
	var res *MessageTokensCount
	err := execute(ctx, "v1/messages/count_tokens", params, &res, r.Options, opts)
	return res, err
}
