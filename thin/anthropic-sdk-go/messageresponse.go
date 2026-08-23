package anthropic

import "encoding/json"

// Response unions are flattened: every variant's fields live on one struct,
// and AsAny re-decodes the raw payload into the concrete variant selected by
// the `type` discriminator.

// Usage reports the token usage of a message.
type Usage struct {
	CacheCreationInputTokens int64               `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64               `json:"cache_read_input_tokens"`
	InputTokens              int64               `json:"input_tokens"`
	OutputTokens             int64               `json:"output_tokens"`
	OutputTokensDetails      OutputTokensDetails `json:"output_tokens_details"`
}

// OutputTokensDetails decomposes the output tokens of a message.
type OutputTokensDetails struct {
	ThinkingTokens int64 `json:"thinking_tokens"`
}

// MessageDeltaUsage is the usage reported on a message_delta event.
type MessageDeltaUsage struct {
	CacheCreationInputTokens int64               `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64               `json:"cache_read_input_tokens"`
	InputTokens              int64               `json:"input_tokens"`
	OutputTokens             int64               `json:"output_tokens"`
	OutputTokensDetails      OutputTokensDetails `json:"output_tokens_details"`
}

// ContentBlockUnion is one content block of a returned message.
type ContentBlockUnion struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`

	raw string
}

func (u ContentBlockUnion) RawJSON() string { return u.raw }

func (u *ContentBlockUnion) UnmarshalJSON(data []byte) error {
	type shadow ContentBlockUnion
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*u = ContentBlockUnion(s)
	u.raw = string(data)
	return nil
}

func (u ContentBlockUnion) AsAny() any {
	switch u.Type {
	case "text":
		return decodeVariant[TextBlock](u.raw)
	case "thinking":
		return decodeVariant[ThinkingBlock](u.raw)
	case "redacted_thinking":
		return decodeVariant[RedactedThinkingBlock](u.raw)
	case "tool_use":
		return decodeVariant[ToolUseBlock](u.raw)
	}
	return nil
}

// TextBlock is a text block of a returned message.
type TextBlock struct {
	Text string    `json:"text"`
	Type constText `json:"type"`
}

// ThinkingBlock is an extended-thinking block of a returned message.
type ThinkingBlock struct {
	Signature string        `json:"signature"`
	Thinking  string        `json:"thinking"`
	Type      constThinking `json:"type"`
}

// RedactedThinkingBlock is a redacted thinking block.
type RedactedThinkingBlock struct {
	Data string                `json:"data"`
	Type constRedactedThinking `json:"type"`
}

// ToolUseBlock is a tool call requested by the model.
type ToolUseBlock struct {
	ID    string          `json:"id"`
	Input json.RawMessage `json:"input"`
	Name  string          `json:"name"`
	Type  constToolUse    `json:"type"`
}

// Message is a message returned by the API.
type Message struct {
	ID           string              `json:"id"`
	Content      []ContentBlockUnion `json:"content"`
	Model        Model               `json:"model"`
	Role         constAssistant      `json:"role"`
	StopReason   StopReason          `json:"stop_reason"`
	StopSequence string              `json:"stop_sequence"`
	Type         constMessage        `json:"type"`
	Usage        Usage               `json:"usage"`
}

// MessageStartEvent starts a streamed message.
type MessageStartEvent struct {
	Message Message           `json:"message"`
	Type    constMessageStart `json:"type"`
}

// ContentBlockStartEventContentBlockUnion is the block opened by a
// content_block_start event.
type ContentBlockStartEventContentBlockUnion = ContentBlockUnion

// ContentBlockStartEvent opens a content block.
type ContentBlockStartEvent struct {
	ContentBlock ContentBlockStartEventContentBlockUnion `json:"content_block"`
	Index        int64                                   `json:"index"`
	Type         constContentBlockStart                  `json:"type"`
}

// RawContentBlockDeltaUnion is the delta of a content_block_delta event.
type RawContentBlockDeltaUnion struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`

	raw string
}

func (u RawContentBlockDeltaUnion) RawJSON() string { return u.raw }

func (u *RawContentBlockDeltaUnion) UnmarshalJSON(data []byte) error {
	type shadow RawContentBlockDeltaUnion
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*u = RawContentBlockDeltaUnion(s)
	u.raw = string(data)
	return nil
}

func (u RawContentBlockDeltaUnion) AsAny() any {
	switch u.Type {
	case "text_delta":
		return decodeVariant[TextDelta](u.raw)
	case "input_json_delta":
		return decodeVariant[InputJSONDelta](u.raw)
	case "thinking_delta":
		return decodeVariant[ThinkingDelta](u.raw)
	case "signature_delta":
		return decodeVariant[SignatureDelta](u.raw)
	}
	return nil
}

// TextDelta appends text to a text block.
type TextDelta struct {
	Text string         `json:"text"`
	Type constTextDelta `json:"type"`
}

// InputJSONDelta appends partial JSON to a tool_use block's input.
type InputJSONDelta struct {
	PartialJSON string              `json:"partial_json"`
	Type        constInputJSONDelta `json:"type"`
}

// ThinkingDelta appends text to a thinking block.
type ThinkingDelta struct {
	Thinking string             `json:"thinking"`
	Type     constThinkingDelta `json:"type"`
}

// SignatureDelta carries the signature of a thinking block.
type SignatureDelta struct {
	Signature string              `json:"signature"`
	Type      constSignatureDelta `json:"type"`
}

// ContentBlockDeltaEvent updates an open content block.
type ContentBlockDeltaEvent struct {
	Delta RawContentBlockDeltaUnion `json:"delta"`
	Index int64                     `json:"index"`
	Type  constContentBlockDelta    `json:"type"`
}

// ContentBlockStopEvent closes a content block.
type ContentBlockStopEvent struct {
	Index int64                 `json:"index"`
	Type  constContentBlockStop `json:"type"`
}

// MessageDeltaEventDelta carries the top-level fields finalized at the end of
// a message.
type MessageDeltaEventDelta struct {
	StopReason   StopReason `json:"stop_reason"`
	StopSequence string     `json:"stop_sequence"`
}

// MessageDeltaEvent finalizes a streamed message.
type MessageDeltaEvent struct {
	Delta MessageDeltaEventDelta `json:"delta"`
	Type  constMessageDelta      `json:"type"`
	Usage MessageDeltaUsage      `json:"usage"`
}

// MessageStopEvent ends a streamed message.
type MessageStopEvent struct {
	Type constMessageStop `json:"type"`
}

// MessageStreamEventUnion is one event of a Messages API stream.
type MessageStreamEventUnion struct {
	Message      Message                                 `json:"message"`
	Type         string                                  `json:"type"`
	Delta        MessageStreamEventUnionDelta            `json:"delta"`
	Usage        MessageDeltaUsage                       `json:"usage"`
	ContentBlock ContentBlockStartEventContentBlockUnion `json:"content_block"`
	Index        int64                                   `json:"index"`

	raw string
}

// MessageStreamEventUnionDelta is the flattened delta of any stream event.
type MessageStreamEventUnionDelta struct {
	StopReason   StopReason `json:"stop_reason"`
	StopSequence string     `json:"stop_sequence"`
	Text         string     `json:"text"`
	Type         string     `json:"type"`
	PartialJSON  string     `json:"partial_json"`
	Thinking     string     `json:"thinking"`
	Signature    string     `json:"signature"`
}

func (u MessageStreamEventUnion) RawJSON() string { return u.raw }

func (u *MessageStreamEventUnion) UnmarshalJSON(data []byte) error {
	type shadow MessageStreamEventUnion
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*u = MessageStreamEventUnion(s)
	u.raw = string(data)
	return nil
}

func (u MessageStreamEventUnion) AsAny() any {
	switch u.Type {
	case "message_start":
		return decodeVariant[MessageStartEvent](u.raw)
	case "message_delta":
		return decodeVariant[MessageDeltaEvent](u.raw)
	case "message_stop":
		return decodeVariant[MessageStopEvent](u.raw)
	case "content_block_start":
		return decodeVariant[ContentBlockStartEvent](u.raw)
	case "content_block_delta":
		return decodeVariant[ContentBlockDeltaEvent](u.raw)
	case "content_block_stop":
		return decodeVariant[ContentBlockStopEvent](u.raw)
	}
	return nil
}

func decodeVariant[T any](raw string) T {
	var v T
	_ = json.Unmarshal([]byte(raw), &v)
	return v
}
