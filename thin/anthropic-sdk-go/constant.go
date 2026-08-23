package anthropic

// The `type` (and `role`) discriminators of API objects are constants: they
// are always serialized, whatever the in-memory value, and are pinned on
// decode. Each literal gets its own type so a struct field carries it
// implicitly.

type constText string

func (constText) MarshalJSON() ([]byte, error)  { return []byte(`"text"`), nil }
func (c *constText) UnmarshalJSON([]byte) error { *c = "text"; return nil }

type constImage string

func (constImage) MarshalJSON() ([]byte, error)  { return []byte(`"image"`), nil }
func (c *constImage) UnmarshalJSON([]byte) error { *c = "image"; return nil }

type constDocument string

func (constDocument) MarshalJSON() ([]byte, error)  { return []byte(`"document"`), nil }
func (c *constDocument) UnmarshalJSON([]byte) error { *c = "document"; return nil }

type constThinking string

func (constThinking) MarshalJSON() ([]byte, error)  { return []byte(`"thinking"`), nil }
func (c *constThinking) UnmarshalJSON([]byte) error { *c = "thinking"; return nil }

type constRedactedThinking string

func (constRedactedThinking) MarshalJSON() ([]byte, error)  { return []byte(`"redacted_thinking"`), nil }
func (c *constRedactedThinking) UnmarshalJSON([]byte) error { *c = "redacted_thinking"; return nil }

type constToolUse string

func (constToolUse) MarshalJSON() ([]byte, error)  { return []byte(`"tool_use"`), nil }
func (c *constToolUse) UnmarshalJSON([]byte) error { *c = "tool_use"; return nil }

type constToolResult string

func (constToolResult) MarshalJSON() ([]byte, error)  { return []byte(`"tool_result"`), nil }
func (c *constToolResult) UnmarshalJSON([]byte) error { *c = "tool_result"; return nil }

type constToolReference string

func (constToolReference) MarshalJSON() ([]byte, error)  { return []byte(`"tool_reference"`), nil }
func (c *constToolReference) UnmarshalJSON([]byte) error { *c = "tool_reference"; return nil }

type constBase64 string

func (constBase64) MarshalJSON() ([]byte, error)  { return []byte(`"base64"`), nil }
func (c *constBase64) UnmarshalJSON([]byte) error { *c = "base64"; return nil }

type constURL string

func (constURL) MarshalJSON() ([]byte, error)  { return []byte(`"url"`), nil }
func (c *constURL) UnmarshalJSON([]byte) error { *c = "url"; return nil }

type constApplicationPDF string

func (constApplicationPDF) MarshalJSON() ([]byte, error)  { return []byte(`"application/pdf"`), nil }
func (c *constApplicationPDF) UnmarshalJSON([]byte) error { *c = "application/pdf"; return nil }

type constEphemeral string

func (constEphemeral) MarshalJSON() ([]byte, error)  { return []byte(`"ephemeral"`), nil }
func (c *constEphemeral) UnmarshalJSON([]byte) error { *c = "ephemeral"; return nil }

type constEnabled string

func (constEnabled) MarshalJSON() ([]byte, error)  { return []byte(`"enabled"`), nil }
func (c *constEnabled) UnmarshalJSON([]byte) error { *c = "enabled"; return nil }

type constDisabled string

func (constDisabled) MarshalJSON() ([]byte, error)  { return []byte(`"disabled"`), nil }
func (c *constDisabled) UnmarshalJSON([]byte) error { *c = "disabled"; return nil }

type constAdaptive string

func (constAdaptive) MarshalJSON() ([]byte, error)  { return []byte(`"adaptive"`), nil }
func (c *constAdaptive) UnmarshalJSON([]byte) error { *c = "adaptive"; return nil }

type constObject string

func (constObject) MarshalJSON() ([]byte, error)  { return []byte(`"object"`), nil }
func (c *constObject) UnmarshalJSON([]byte) error { *c = "object"; return nil }

type constJSONSchema string

func (constJSONSchema) MarshalJSON() ([]byte, error)  { return []byte(`"json_schema"`), nil }
func (c *constJSONSchema) UnmarshalJSON([]byte) error { *c = "json_schema"; return nil }

type constAssistant string

func (constAssistant) MarshalJSON() ([]byte, error)  { return []byte(`"assistant"`), nil }
func (c *constAssistant) UnmarshalJSON([]byte) error { *c = "assistant"; return nil }

type constMessage string

func (constMessage) MarshalJSON() ([]byte, error)  { return []byte(`"message"`), nil }
func (c *constMessage) UnmarshalJSON([]byte) error { *c = "message"; return nil }

type constMessageStart string

func (constMessageStart) MarshalJSON() ([]byte, error)  { return []byte(`"message_start"`), nil }
func (c *constMessageStart) UnmarshalJSON([]byte) error { *c = "message_start"; return nil }

type constMessageDelta string

func (constMessageDelta) MarshalJSON() ([]byte, error)  { return []byte(`"message_delta"`), nil }
func (c *constMessageDelta) UnmarshalJSON([]byte) error { *c = "message_delta"; return nil }

type constMessageStop string

func (constMessageStop) MarshalJSON() ([]byte, error)  { return []byte(`"message_stop"`), nil }
func (c *constMessageStop) UnmarshalJSON([]byte) error { *c = "message_stop"; return nil }

type constContentBlockStart string

func (constContentBlockStart) MarshalJSON() ([]byte, error) {
	return []byte(`"content_block_start"`), nil
}

func (c *constContentBlockStart) UnmarshalJSON([]byte) error { *c = "content_block_start"; return nil }

type constContentBlockDelta string

func (constContentBlockDelta) MarshalJSON() ([]byte, error) {
	return []byte(`"content_block_delta"`), nil
}

func (c *constContentBlockDelta) UnmarshalJSON([]byte) error { *c = "content_block_delta"; return nil }

type constContentBlockStop string

func (constContentBlockStop) MarshalJSON() ([]byte, error) {
	return []byte(`"content_block_stop"`), nil
}

func (c *constContentBlockStop) UnmarshalJSON([]byte) error { *c = "content_block_stop"; return nil }

type constTextDelta string

func (constTextDelta) MarshalJSON() ([]byte, error)  { return []byte(`"text_delta"`), nil }
func (c *constTextDelta) UnmarshalJSON([]byte) error { *c = "text_delta"; return nil }

type constInputJSONDelta string

func (constInputJSONDelta) MarshalJSON() ([]byte, error)  { return []byte(`"input_json_delta"`), nil }
func (c *constInputJSONDelta) UnmarshalJSON([]byte) error { *c = "input_json_delta"; return nil }

type constThinkingDelta string

func (constThinkingDelta) MarshalJSON() ([]byte, error)  { return []byte(`"thinking_delta"`), nil }
func (c *constThinkingDelta) UnmarshalJSON([]byte) error { *c = "thinking_delta"; return nil }

type constSignatureDelta string

func (constSignatureDelta) MarshalJSON() ([]byte, error)  { return []byte(`"signature_delta"`), nil }
func (c *constSignatureDelta) UnmarshalJSON([]byte) error { *c = "signature_delta"; return nil }
