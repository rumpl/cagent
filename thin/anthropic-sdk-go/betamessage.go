package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// Beta feature flags sent in the `anthropic-beta` header.
const (
	AnthropicBetaInterleavedThinking2025_05_14      AnthropicBeta = "interleaved-thinking-2025-05-14"
	AnthropicBetaFineGrainedToolStreaming2025_05_14 AnthropicBeta = "fine-grained-tool-streaming-2025-05-14"
)

// BetaStopReason is why the model stopped generating, on the Beta API.
type BetaStopReason string

const (
	BetaStopReasonEndTurn      BetaStopReason = "end_turn"
	BetaStopReasonMaxTokens    BetaStopReason = "max_tokens"
	BetaStopReasonStopSequence BetaStopReason = "stop_sequence"
	BetaStopReasonToolUse      BetaStopReason = "tool_use"
	BetaStopReasonPauseTurn    BetaStopReason = "pause_turn"
	BetaStopReasonRefusal      BetaStopReason = "refusal"
)

// BetaMessageParamRole is the role of an input message.
type BetaMessageParamRole string

const (
	BetaMessageParamRoleUser      BetaMessageParamRole = "user"
	BetaMessageParamRoleAssistant BetaMessageParamRole = "assistant"
)

// BetaCacheControlEphemeralTTL is the lifetime of a cache breakpoint.
type BetaCacheControlEphemeralTTL string

// BetaBase64ImageSourceMediaType is the MIME type of an inline image.
type BetaBase64ImageSourceMediaType string

// BetaThinkingConfigEnabledDisplay controls how token-budget thinking is shown.
type BetaThinkingConfigEnabledDisplay string

// BetaThinkingConfigAdaptiveDisplay controls how adaptive thinking is shown.
type BetaThinkingConfigAdaptiveDisplay string

// BetaOutputConfigEffort is the reasoning-effort level of a request.
type BetaOutputConfigEffort string

const (
	BetaOutputConfigEffortLow    BetaOutputConfigEffort = "low"
	BetaOutputConfigEffortMedium BetaOutputConfigEffort = "medium"
	BetaOutputConfigEffortHigh   BetaOutputConfigEffort = "high"
	BetaOutputConfigEffortXhigh  BetaOutputConfigEffort = "xhigh"
	BetaOutputConfigEffortMax    BetaOutputConfigEffort = "max"
)

// BetaToolType is the kind of a custom tool.
type BetaToolType string

const BetaToolTypeCustom BetaToolType = "custom"

// BetaCacheControlEphemeralParam marks a prompt-caching breakpoint.
type BetaCacheControlEphemeralParam struct {
	TTL  BetaCacheControlEphemeralTTL `json:"ttl,omitzero"`
	Type constEphemeral               `json:"type"`
}

func NewBetaCacheControlEphemeralParam() BetaCacheControlEphemeralParam {
	return BetaCacheControlEphemeralParam{Type: "ephemeral"}
}

// BetaTextBlockParam is a text content block.
type BetaTextBlockParam struct {
	Text         string                         `json:"text"`
	CacheControl BetaCacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constText                      `json:"type"`
}

// BetaBase64ImageSourceParam is an inline base64-encoded image.
type BetaBase64ImageSourceParam struct {
	Data      string                         `json:"data"`
	MediaType BetaBase64ImageSourceMediaType `json:"media_type,omitzero"`
	Type      constBase64                    `json:"type"`
}

// BetaURLImageSourceParam references an image by URL.
type BetaURLImageSourceParam struct {
	URL  string   `json:"url"`
	Type constURL `json:"type"`
}

// BetaImageBlockParamSourceUnion is the source of a [BetaImageBlockParam].
type BetaImageBlockParamSourceUnion struct {
	OfBase64 *BetaBase64ImageSourceParam `json:",omitzero,inline"`
	OfURL    *BetaURLImageSourceParam    `json:",omitzero,inline"`

	paramUnion
}

func (u BetaImageBlockParamSourceUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfBase64, u.OfURL)
}

func (u BetaImageBlockParamSourceUnion) IsZero() bool { return u.OfBase64 == nil && u.OfURL == nil }

// BetaImageBlockParam is an image content block.
type BetaImageBlockParam struct {
	Source       BetaImageBlockParamSourceUnion `json:"source,omitzero"`
	CacheControl BetaCacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constImage                     `json:"type"`
}

// BetaBase64PDFSourceParam is an inline base64-encoded PDF.
type BetaBase64PDFSourceParam struct {
	Data      string              `json:"data"`
	MediaType constApplicationPDF `json:"media_type"`
	Type      constBase64         `json:"type"`
}

// BetaRequestDocumentBlockSourceUnionParam is the source of a document block.
type BetaRequestDocumentBlockSourceUnionParam struct {
	OfBase64 *BetaBase64PDFSourceParam `json:",omitzero,inline"`

	paramUnion
}

func (u BetaRequestDocumentBlockSourceUnionParam) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfBase64)
}

func (u BetaRequestDocumentBlockSourceUnionParam) IsZero() bool { return u.OfBase64 == nil }

// BetaRequestDocumentBlockParam is a document (PDF) content block.
type BetaRequestDocumentBlockParam struct {
	Source       BetaRequestDocumentBlockSourceUnionParam `json:"source,omitzero"`
	Context      param.Opt[string]                        `json:"context,omitzero"`
	Title        param.Opt[string]                        `json:"title,omitzero"`
	CacheControl BetaCacheControlEphemeralParam           `json:"cache_control,omitzero"`
	Type         constDocument                            `json:"type"`
}

// BetaThinkingBlockParam replays a previous extended-thinking block.
type BetaThinkingBlockParam struct {
	Signature string        `json:"signature"`
	Thinking  string        `json:"thinking"`
	Type      constThinking `json:"type"`
}

// BetaRedactedThinkingBlockParam replays a redacted thinking block.
type BetaRedactedThinkingBlockParam struct {
	Data string                `json:"data"`
	Type constRedactedThinking `json:"type"`
}

// BetaToolUseBlockParam replays a tool call made by the model.
type BetaToolUseBlockParam struct {
	ID           string                         `json:"id"`
	Input        any                            `json:"input,omitzero"`
	Name         string                         `json:"name"`
	CacheControl BetaCacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constToolUse                   `json:"type"`
}

// BetaToolReferenceBlockParam activates a deferred tool by name.
type BetaToolReferenceBlockParam struct {
	ToolName     string                         `json:"tool_name"`
	CacheControl BetaCacheControlEphemeralParam `json:"cache_control,omitzero"`
	Type         constToolReference             `json:"type"`
}

// BetaToolResultBlockParamContentUnion is one block of a tool result.
type BetaToolResultBlockParamContentUnion struct {
	OfText          *BetaTextBlockParam            `json:",omitzero,inline"`
	OfImage         *BetaImageBlockParam           `json:",omitzero,inline"`
	OfDocument      *BetaRequestDocumentBlockParam `json:",omitzero,inline"`
	OfToolReference *BetaToolReferenceBlockParam   `json:",omitzero,inline"`

	paramUnion
}

func (u BetaToolResultBlockParamContentUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfText, u.OfImage, u.OfDocument, u.OfToolReference)
}

func (u BetaToolResultBlockParamContentUnion) IsZero() bool {
	return u.OfText == nil && u.OfImage == nil && u.OfDocument == nil && u.OfToolReference == nil
}

// BetaToolResultBlockParam returns the result of a tool call to the model.
type BetaToolResultBlockParam struct {
	ToolUseID    string                                 `json:"tool_use_id"`
	IsError      param.Opt[bool]                        `json:"is_error,omitzero"`
	CacheControl BetaCacheControlEphemeralParam         `json:"cache_control,omitzero"`
	Content      []BetaToolResultBlockParamContentUnion `json:"content,omitzero"`
	Type         constToolResult                        `json:"type"`
}

// BetaContentBlockParamUnion is one content block of an input message.
type BetaContentBlockParamUnion struct {
	OfText             *BetaTextBlockParam             `json:",omitzero,inline"`
	OfImage            *BetaImageBlockParam            `json:",omitzero,inline"`
	OfDocument         *BetaRequestDocumentBlockParam  `json:",omitzero,inline"`
	OfThinking         *BetaThinkingBlockParam         `json:",omitzero,inline"`
	OfRedactedThinking *BetaRedactedThinkingBlockParam `json:",omitzero,inline"`
	OfToolUse          *BetaToolUseBlockParam          `json:",omitzero,inline"`
	OfToolResult       *BetaToolResultBlockParam       `json:",omitzero,inline"`

	paramUnion
}

func (u BetaContentBlockParamUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfText, u.OfImage, u.OfDocument, u.OfThinking, u.OfRedactedThinking, u.OfToolUse, u.OfToolResult)
}

func (u BetaContentBlockParamUnion) IsZero() bool {
	return u.OfText == nil && u.OfImage == nil && u.OfDocument == nil && u.OfThinking == nil &&
		u.OfRedactedThinking == nil && u.OfToolUse == nil && u.OfToolResult == nil
}

// GetCacheControl returns a pointer to the block's cache-control field, or
// nil for block kinds that cannot carry a breakpoint (thinking blocks).
func (u BetaContentBlockParamUnion) GetCacheControl() *BetaCacheControlEphemeralParam {
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

// BetaMessageParam is one input message of a conversation.
type BetaMessageParam struct {
	Content []BetaContentBlockParamUnion `json:"content,omitzero"`
	Role    BetaMessageParamRole         `json:"role,omitzero"`
}

func NewBetaUserMessage(blocks ...BetaContentBlockParamUnion) BetaMessageParam {
	return BetaMessageParam{Role: BetaMessageParamRoleUser, Content: blocks}
}

func NewBetaAssistantMessage(blocks ...BetaContentBlockParamUnion) BetaMessageParam {
	return BetaMessageParam{Role: BetaMessageParamRoleAssistant, Content: blocks}
}

func NewBetaTextBlock(text string) BetaContentBlockParamUnion {
	return BetaContentBlockParamUnion{OfText: &BetaTextBlockParam{Text: text}}
}

func NewBetaThinkingBlock(signature, thinking string) BetaContentBlockParamUnion {
	return BetaContentBlockParamUnion{OfThinking: &BetaThinkingBlockParam{Signature: signature, Thinking: thinking}}
}

func NewBetaRedactedThinkingBlock(data string) BetaContentBlockParamUnion {
	return BetaContentBlockParamUnion{OfRedactedThinking: &BetaRedactedThinkingBlockParam{Data: data}}
}

func NewBetaToolResultBlock(toolUseID, content string, isError bool) BetaContentBlockParamUnion {
	return BetaContentBlockParamUnion{OfToolResult: &BetaToolResultBlockParam{
		ToolUseID: toolUseID,
		Content:   []BetaToolResultBlockParamContentUnion{{OfText: &BetaTextBlockParam{Text: content}}},
		IsError:   Bool(isError),
	}}
}

// BetaToolInputSchemaParam is the JSON Schema of a tool's input.
type BetaToolInputSchemaParam struct {
	Properties  any            `json:"properties,omitzero"`
	Required    []string       `json:"required,omitzero"`
	Type        constObject    `json:"type"`
	ExtraFields map[string]any `json:"-"`
}

func (r BetaToolInputSchemaParam) MarshalJSON() ([]byte, error) {
	type shadow BetaToolInputSchemaParam
	return marshalWithExtras(shadow(r), r.ExtraFields)
}

// BetaToolParam is a custom tool definition.
type BetaToolParam struct {
	InputSchema  BetaToolInputSchemaParam       `json:"input_schema,omitzero"`
	Name         string                         `json:"name"`
	DeferLoading param.Opt[bool]                `json:"defer_loading,omitzero"`
	Description  param.Opt[string]              `json:"description,omitzero"`
	Strict       param.Opt[bool]                `json:"strict,omitzero"`
	Type         BetaToolType                   `json:"type,omitzero"`
	CacheControl BetaCacheControlEphemeralParam `json:"cache_control,omitzero"`
}

// BetaToolUnionParam is one entry of a request's `tools` array.
type BetaToolUnionParam struct {
	OfTool *BetaToolParam `json:",omitzero,inline"`

	paramUnion
}

func (u BetaToolUnionParam) MarshalJSON() ([]byte, error) { return marshalUnion(u.OfTool) }
func (u BetaToolUnionParam) IsZero() bool                 { return u.OfTool == nil }

// BetaMessageCountTokensParamsToolUnion is one entry of a count-tokens `tools` array.
type BetaMessageCountTokensParamsToolUnion struct {
	OfTool *BetaToolParam `json:",omitzero,inline"`

	paramUnion
}

func (u BetaMessageCountTokensParamsToolUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfTool)
}

func (u BetaMessageCountTokensParamsToolUnion) IsZero() bool { return u.OfTool == nil }

// BetaThinkingConfigEnabledParam requests token-budget extended thinking.
type BetaThinkingConfigEnabledParam struct {
	BudgetTokens int64                            `json:"budget_tokens"`
	Display      BetaThinkingConfigEnabledDisplay `json:"display,omitzero"`
	Type         constEnabled                     `json:"type"`
}

// BetaThinkingConfigAdaptiveParam requests adaptive (model-managed) thinking.
type BetaThinkingConfigAdaptiveParam struct {
	Display BetaThinkingConfigAdaptiveDisplay `json:"display,omitzero"`
	Type    constAdaptive                     `json:"type"`
}

// BetaThinkingConfigDisabledParam disables extended thinking.
type BetaThinkingConfigDisabledParam struct {
	Type constDisabled `json:"type"`
}

// BetaThinkingConfigParamUnion is the request's `thinking` field.
type BetaThinkingConfigParamUnion struct {
	OfEnabled  *BetaThinkingConfigEnabledParam  `json:",omitzero,inline"`
	OfDisabled *BetaThinkingConfigDisabledParam `json:",omitzero,inline"`
	OfAdaptive *BetaThinkingConfigAdaptiveParam `json:",omitzero,inline"`

	paramUnion
}

func (u BetaThinkingConfigParamUnion) MarshalJSON() ([]byte, error) {
	return marshalUnion(u.OfEnabled, u.OfDisabled, u.OfAdaptive)
}

func (u BetaThinkingConfigParamUnion) IsZero() bool {
	return u.OfEnabled == nil && u.OfDisabled == nil && u.OfAdaptive == nil
}

func BetaThinkingConfigParamOfEnabled(budgetTokens int64) BetaThinkingConfigParamUnion {
	return BetaThinkingConfigParamUnion{OfEnabled: &BetaThinkingConfigEnabledParam{BudgetTokens: budgetTokens}}
}

// BetaJSONOutputFormatParam constrains the response to a JSON schema.
type BetaJSONOutputFormatParam struct {
	Schema any             `json:"schema,omitzero"`
	Type   constJSONSchema `json:"type"`
}

func (r BetaJSONOutputFormatParam) IsZero() bool { return r.Schema == nil }

// BetaJSONSchemaOutputFormat builds an output format from a JSON schema map.
func BetaJSONSchemaOutputFormat(jsonSchema map[string]any) BetaJSONOutputFormatParam {
	return BetaJSONOutputFormatParam{Schema: jsonSchema}
}

// BetaOutputConfigParam is the request's `output_config` field.
type BetaOutputConfigParam struct {
	Effort BetaOutputConfigEffort    `json:"effort,omitzero"`
	Format BetaJSONOutputFormatParam `json:"format,omitzero"`

	extras map[string]any
}

// SetExtraFields adds fields to the serialized object, overriding any
// existing key. Only use it with trusted input.
func (r *BetaOutputConfigParam) SetExtraFields(extraFields map[string]any) { r.extras = extraFields }

func (r BetaOutputConfigParam) MarshalJSON() ([]byte, error) {
	type shadow BetaOutputConfigParam
	return marshalWithExtras(shadow(r), r.extras)
}

func (r BetaOutputConfigParam) IsZero() bool {
	return r.Effort == "" && r.Format.IsZero() && len(r.extras) == 0
}

// BetaMessageCountTokensParamsSystemUnion is the count-tokens `system` field.
type BetaMessageCountTokensParamsSystemUnion struct {
	OfString             param.Opt[string]    `json:",omitzero,inline"`
	OfBetaTextBlockArray []BetaTextBlockParam `json:",omitzero,inline"`

	paramUnion
}

func (u BetaMessageCountTokensParamsSystemUnion) MarshalJSON() ([]byte, error) {
	if u.OfString.Valid() {
		return json.Marshal(u.OfString)
	}
	return json.Marshal(u.OfBetaTextBlockArray)
}

func (u BetaMessageCountTokensParamsSystemUnion) IsZero() bool {
	return !u.OfString.Valid() && u.OfBetaTextBlockArray == nil
}

// BetaMessageNewParams is the body of a POST /v1/messages?beta=true request.
// Betas is sent as `anthropic-beta` headers, not in the body.
type BetaMessageNewParams struct {
	MaxTokens     int64                          `json:"max_tokens"`
	Messages      []BetaMessageParam             `json:"messages,omitzero"`
	Model         Model                          `json:"model,omitzero"`
	Temperature   param.Opt[float64]             `json:"temperature,omitzero"`
	TopK          param.Opt[int64]               `json:"top_k,omitzero"`
	TopP          param.Opt[float64]             `json:"top_p,omitzero"`
	CacheControl  BetaCacheControlEphemeralParam `json:"cache_control,omitzero"`
	OutputConfig  BetaOutputConfigParam          `json:"output_config,omitzero"`
	StopSequences []string                       `json:"stop_sequences,omitzero"`
	System        []BetaTextBlockParam           `json:"system,omitzero"`
	Thinking      BetaThinkingConfigParamUnion   `json:"thinking,omitzero"`
	Tools         []BetaToolUnionParam           `json:"tools,omitzero"`
	Betas         []AnthropicBeta                `json:"-"`
}

// BetaMessageCountTokensParams is the body of a beta count_tokens request.
type BetaMessageCountTokensParams struct {
	Messages     []BetaMessageParam                      `json:"messages,omitzero"`
	Model        Model                                   `json:"model,omitzero"`
	OutputConfig BetaOutputConfigParam                   `json:"output_config,omitzero"`
	System       BetaMessageCountTokensParamsSystemUnion `json:"system,omitzero"`
	Thinking     BetaThinkingConfigParamUnion            `json:"thinking,omitzero"`
	Tools        []BetaMessageCountTokensParamsToolUnion `json:"tools,omitzero"`
	Betas        []AnthropicBeta                         `json:"-"`
}

// BetaMessageTokensCount is the response of a beta count_tokens request.
type BetaMessageTokensCount struct {
	InputTokens int64 `json:"input_tokens"`
}

func betaHeaderOptions(betas []AnthropicBeta, opts []option.RequestOption) []option.RequestOption {
	out := make([]option.RequestOption, 0, len(betas)+len(opts))
	for _, v := range betas {
		out = append(out, option.WithHeaderAdd("anthropic-beta", fmt.Sprintf("%v", v)))
	}
	return append(out, opts...)
}

// NewStreaming posts a message to the Beta API and returns the event stream.
func (r *BetaMessageService) NewStreaming(ctx context.Context, params BetaMessageNewParams, opts ...option.RequestOption) *ssestream.Stream[BetaRawMessageStreamEventUnion] {
	var raw *http.Response
	opts = append(betaHeaderOptions(params.Betas, opts), option.WithJSONSet("stream", true))
	err := execute(ctx, "v1/messages?beta=true", params, &raw, r.Options, opts)
	return ssestream.NewStream[BetaRawMessageStreamEventUnion](ssestream.NewDecoder(raw), err)
}

// CountTokens counts the tokens a Beta API message would consume.
func (r *BetaMessageService) CountTokens(ctx context.Context, params BetaMessageCountTokensParams, opts ...option.RequestOption) (*BetaMessageTokensCount, error) {
	var res *BetaMessageTokensCount
	err := execute(ctx, "v1/messages/count_tokens?beta=true", params, &res, r.Options, betaHeaderOptions(params.Betas, opts))
	return res, err
}
