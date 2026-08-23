package responses

import (
	"encoding/json"

	"github.com/openai/openai-go/v3/internal/apijson"
	"github.com/openai/openai-go/v3/internal/encjson"
	"github.com/openai/openai-go/v3/packages/respjson"
)

// ResponseStreamEventUnion is one event of a streamed response. Like the
// upstream SDK it is a flattened union: every variant's fields live on the
// same struct, and Type says which of them are meaningful. JSON records which
// fields the server actually sent, so a legitimate zero (output_index 0) is
// distinguishable from an absent field.
type ResponseStreamEventUnion struct {
	Type           string   `json:"type"`
	SequenceNumber int64    `json:"sequence_number"`
	Delta          string   `json:"delta"`
	Text           string   `json:"text"`
	Code           string   `json:"code"`
	ItemID         string   `json:"item_id"`
	OutputIndex    int64    `json:"output_index"`
	ContentIndex   int64    `json:"content_index"`
	SummaryIndex   int64    `json:"summary_index"`
	Arguments      string   `json:"arguments"`
	Name           string   `json:"name"`
	Refusal        string   `json:"refusal"`
	Status         string   `json:"status"`
	Message        string   `json:"message"`
	Param          string   `json:"param"`
	Obfuscation    string   `json:"obfuscation"`
	Response       Response `json:"response"`
	// Part is the content part of a content_part.added / .done event.
	Part ResponseStreamEventUnionPart `json:"part"`
	// Item is the output item of an output_item.added / .done event.
	Item ResponseOutputItemUnion `json:"item"`
	JSON struct {
		Type           respjson.Field
		SequenceNumber respjson.Field
		Delta          respjson.Field
		Text           respjson.Field
		Code           respjson.Field
		ItemID         respjson.Field
		OutputIndex    respjson.Field
		ContentIndex   respjson.Field
		SummaryIndex   respjson.Field
		Arguments      respjson.Field
		Name           respjson.Field
		Refusal        respjson.Field
		Status         respjson.Field
		Message        respjson.Field
		Param          respjson.Field
		Obfuscation    respjson.Field
		Response       respjson.Field
		Part           respjson.Field
		Item           respjson.Field
		ExtraFields    map[string]respjson.Field
		Raw            string
	} `json:"-"`
}

func (r ResponseStreamEventUnion) RawJSON() string { return r.JSON.Raw }

func (r *ResponseStreamEventUnion) UnmarshalJSON(data []byte) error {
	return apijson.UnmarshalRoot(data, r)
}

// ResponseStreamEventUnionPart is a content part carried by a stream event.
type ResponseStreamEventUnionPart struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

// ResponseOutputItemUnion is an item of the response output: a message, a
// function call, a reasoning item, ...
type ResponseOutputItemUnion struct {
	ID               string                              `json:"id"`
	Type             string                              `json:"type"`
	Role             string                              `json:"role"`
	Status           string                              `json:"status"`
	Content          []ResponseOutputMessageContentUnion `json:"content"`
	Arguments        ResponseOutputItemUnionArguments    `json:"arguments"`
	CallID           string                              `json:"call_id"`
	Name             string                              `json:"name"`
	Summary          []ResponseReasoningItemSummary      `json:"summary"`
	EncryptedContent string                              `json:"encrypted_content"`
}

// ResponseOutputMessageContentUnion is one block of an output message.
type ResponseOutputMessageContentUnion struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

// ResponseReasoningItemSummary is one summary block of a reasoning item.
type ResponseReasoningItemSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ResponseOutputItemUnionArguments holds a function call's arguments, which
// are a JSON string for function calls and an object for tool searches.
type ResponseOutputItemUnionArguments struct {
	OfString                          string
	OfResponseToolSearchCallArguments any
}

func (a *ResponseOutputItemUnionArguments) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		a.OfString = text
		return nil
	}
	return json.Unmarshal(data, &a.OfResponseToolSearchCallArguments)
}

func (a ResponseOutputItemUnionArguments) MarshalJSON() ([]byte, error) {
	if a.OfString != "" {
		return encjson.Marshal(a.OfString)
	}
	return encjson.Marshal(a.OfResponseToolSearchCallArguments)
}

// Response is a full response object, as delivered by the terminal stream
// events and by the non-streaming endpoint.
type Response struct {
	ID                 string                    `json:"id"`
	CreatedAt          float64                   `json:"created_at"`
	Model              string                    `json:"model"`
	Object             string                    `json:"object"`
	Status             string                    `json:"status"`
	Output             []ResponseOutputItemUnion `json:"output"`
	Usage              ResponseUsage             `json:"usage"`
	Error              ResponseError             `json:"error"`
	IncompleteDetails  ResponseIncompleteDetails `json:"incomplete_details"`
	PreviousResponseID string                    `json:"previous_response_id"`
	Instructions       json.RawMessage           `json:"instructions"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResponseIncompleteDetails struct {
	Reason string `json:"reason"`
}

// ResponseUsage reports the token counts of a response.
type ResponseUsage struct {
	InputTokens         int64                            `json:"input_tokens"`
	InputTokensDetails  ResponseUsageInputTokensDetails  `json:"input_tokens_details"`
	OutputTokens        int64                            `json:"output_tokens"`
	OutputTokensDetails ResponseUsageOutputTokensDetails `json:"output_tokens_details"`
	TotalTokens         int64                            `json:"total_tokens"`
}

type ResponseUsageInputTokensDetails struct {
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
}

type ResponseUsageOutputTokensDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}
