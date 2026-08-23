package anthropic

import "encoding/json"

// BetaUsage reports the token usage of a Beta API message.
type BetaUsage struct {
	CacheCreationInputTokens int64                   `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64                   `json:"cache_read_input_tokens"`
	InputTokens              int64                   `json:"input_tokens"`
	OutputTokens             int64                   `json:"output_tokens"`
	OutputTokensDetails      BetaOutputTokensDetails `json:"output_tokens_details"`
}

// BetaOutputTokensDetails decomposes the output tokens of a message.
type BetaOutputTokensDetails struct {
	ThinkingTokens int64 `json:"thinking_tokens"`
}

// BetaMessageDeltaUsage is the usage reported on a message_delta event.
type BetaMessageDeltaUsage struct {
	CacheCreationInputTokens int64                   `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64                   `json:"cache_read_input_tokens"`
	InputTokens              int64                   `json:"input_tokens"`
	OutputTokens             int64                   `json:"output_tokens"`
	OutputTokensDetails      BetaOutputTokensDetails `json:"output_tokens_details"`
}

// BetaContentBlockUnion is one content block of a returned message. The raw
// payload is preserved so a decoded message re-serializes unchanged.
type BetaContentBlockUnion struct {
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

func (u BetaContentBlockUnion) RawJSON() string { return u.raw }

func (u *BetaContentBlockUnion) UnmarshalJSON(data []byte) error {
	type shadow BetaContentBlockUnion
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*u = BetaContentBlockUnion(s)
	u.raw = string(data)
	return nil
}

func (u BetaContentBlockUnion) MarshalJSON() ([]byte, error) {
	if u.raw != "" {
		return []byte(u.raw), nil
	}
	type shadow BetaContentBlockUnion
	return json.Marshal(shadow(u))
}

func (u BetaContentBlockUnion) AsAny() any {
	switch u.Type {
	case "text":
		return decodeVariant[BetaTextBlock](u.raw)
	case "thinking":
		return decodeVariant[BetaThinkingBlock](u.raw)
	case "redacted_thinking":
		return decodeVariant[BetaRedactedThinkingBlock](u.raw)
	case "tool_use":
		return decodeVariant[BetaToolUseBlock](u.raw)
	}
	return nil
}

// BetaTextBlock is a text block of a returned message.
type BetaTextBlock struct {
	Text string    `json:"text"`
	Type constText `json:"type"`
}

// BetaThinkingBlock is an extended-thinking block of a returned message.
type BetaThinkingBlock struct {
	Signature string        `json:"signature"`
	Thinking  string        `json:"thinking"`
	Type      constThinking `json:"type"`
}

// BetaRedactedThinkingBlock is a redacted thinking block.
type BetaRedactedThinkingBlock struct {
	Data string                `json:"data"`
	Type constRedactedThinking `json:"type"`
}

// BetaToolUseBlock is a tool call requested by the model.
type BetaToolUseBlock struct {
	ID    string       `json:"id"`
	Input any          `json:"input"`
	Name  string       `json:"name"`
	Type  constToolUse `json:"type"`
}

// BetaMessage is a message returned by the Beta API.
type BetaMessage struct {
	ID           string                  `json:"id"`
	Content      []BetaContentBlockUnion `json:"content"`
	Model        Model                   `json:"model"`
	Role         constAssistant          `json:"role"`
	StopReason   BetaStopReason          `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence"`
	Type         constMessage            `json:"type"`
	Usage        BetaUsage               `json:"usage"`
}

// BetaRawMessageStartEvent starts a streamed message.
type BetaRawMessageStartEvent struct {
	Message BetaMessage       `json:"message"`
	Type    constMessageStart `json:"type"`
}

// BetaRawContentBlockStartEvent opens a content block.
type BetaRawContentBlockStartEvent struct {
	ContentBlock BetaContentBlockUnion  `json:"content_block"`
	Index        int64                  `json:"index"`
	Type         constContentBlockStart `json:"type"`
}

// BetaRawContentBlockDelta is the delta of a content_block_delta event.
type BetaRawContentBlockDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`

	raw string
}

func (u BetaRawContentBlockDelta) RawJSON() string { return u.raw }

func (u *BetaRawContentBlockDelta) UnmarshalJSON(data []byte) error {
	type shadow BetaRawContentBlockDelta
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*u = BetaRawContentBlockDelta(s)
	u.raw = string(data)
	return nil
}

func (u BetaRawContentBlockDelta) AsAny() any {
	switch u.Type {
	case "text_delta":
		return decodeVariant[BetaTextDelta](u.raw)
	case "input_json_delta":
		return decodeVariant[BetaInputJSONDelta](u.raw)
	case "thinking_delta":
		return decodeVariant[BetaThinkingDelta](u.raw)
	case "signature_delta":
		return decodeVariant[BetaSignatureDelta](u.raw)
	}
	return nil
}

// BetaRawContentBlockDeltaUnion is the delta union of a Beta stream event.
type BetaRawContentBlockDeltaUnion = BetaRawContentBlockDelta

// BetaTextDelta appends text to a text block.
type BetaTextDelta struct {
	Text string         `json:"text"`
	Type constTextDelta `json:"type"`
}

// BetaInputJSONDelta appends partial JSON to a tool_use block's input.
type BetaInputJSONDelta struct {
	PartialJSON string              `json:"partial_json"`
	Type        constInputJSONDelta `json:"type"`
}

// BetaThinkingDelta appends text to a thinking block.
type BetaThinkingDelta struct {
	Thinking string             `json:"thinking"`
	Type     constThinkingDelta `json:"type"`
}

// BetaSignatureDelta carries the signature of a thinking block.
type BetaSignatureDelta struct {
	Signature string              `json:"signature"`
	Type      constSignatureDelta `json:"type"`
}

// BetaRawContentBlockDeltaEvent updates an open content block.
type BetaRawContentBlockDeltaEvent struct {
	Delta BetaRawContentBlockDelta `json:"delta"`
	Index int64                    `json:"index"`
	Type  constContentBlockDelta   `json:"type"`
}

// BetaRawContentBlockStopEvent closes a content block.
type BetaRawContentBlockStopEvent struct {
	Index int64                 `json:"index"`
	Type  constContentBlockStop `json:"type"`
}

// BetaRawMessageDeltaEventDelta carries the fields finalized at the end of a
// message.
type BetaRawMessageDeltaEventDelta struct {
	StopReason   BetaStopReason `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence"`
}

// BetaRawMessageDeltaEvent finalizes a streamed message.
type BetaRawMessageDeltaEvent struct {
	Delta BetaRawMessageDeltaEventDelta `json:"delta"`
	Type  constMessageDelta             `json:"type"`
	Usage BetaMessageDeltaUsage         `json:"usage"`
}

// BetaRawMessageStopEvent ends a streamed message.
type BetaRawMessageStopEvent struct {
	Type constMessageStop `json:"type"`
}

// BetaRawMessageStreamEventUnion is one event of a Beta Messages API stream.
type BetaRawMessageStreamEventUnion struct {
	Message      BetaMessage                      `json:"message"`
	Type         string                           `json:"type"`
	Delta        BetaRawMessageStreamEventerDelta `json:"delta"`
	Usage        BetaMessageDeltaUsage            `json:"usage"`
	ContentBlock BetaContentBlockUnion            `json:"content_block"`
	Index        int64                            `json:"index"`

	raw string
}

// BetaRawMessageStreamEventerDelta is the flattened delta of any Beta event.
type BetaRawMessageStreamEventerDelta struct {
	StopReason   BetaStopReason `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence"`
	Text         string         `json:"text"`
	Type         string         `json:"type"`
	PartialJSON  string         `json:"partial_json"`
	Thinking     string         `json:"thinking"`
	Signature    string         `json:"signature"`
}

func (u BetaRawMessageStreamEventUnion) RawJSON() string { return u.raw }

func (u *BetaRawMessageStreamEventUnion) UnmarshalJSON(data []byte) error {
	type shadow BetaRawMessageStreamEventUnion
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*u = BetaRawMessageStreamEventUnion(s)
	u.raw = string(data)
	return nil
}

func (u BetaRawMessageStreamEventUnion) AsAny() any {
	switch u.Type {
	case "message_start":
		return decodeVariant[BetaRawMessageStartEvent](u.raw)
	case "message_delta":
		return decodeVariant[BetaRawMessageDeltaEvent](u.raw)
	case "message_stop":
		return decodeVariant[BetaRawMessageStopEvent](u.raw)
	case "content_block_start":
		return decodeVariant[BetaRawContentBlockStartEvent](u.raw)
	case "content_block_delta":
		return decodeVariant[BetaRawContentBlockDeltaEvent](u.raw)
	case "content_block_stop":
		return decodeVariant[BetaRawContentBlockStopEvent](u.raw)
	}
	return nil
}
