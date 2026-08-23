// Package openai is a hand-written client for the subset of the OpenAI API
// used by docker-agent: chat completions, responses, embeddings and models.
package openai

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openai/openai-go/v3/internal/apierror"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// Error is returned for any non-2xx API response.
type Error = apierror.Error

// Aliases for the types shared with the other API surfaces.
type (
	ChatModel                               = shared.ChatModel
	ErrorObject                             = shared.ErrorObject
	FunctionDefinitionParam                 = shared.FunctionDefinitionParam
	FunctionParameters                      = shared.FunctionParameters
	Metadata                                = shared.Metadata
	ReasoningEffort                         = shared.ReasoningEffort
	ReasoningParam                          = shared.ReasoningParam
	ResponseFormatJSONObjectParam           = shared.ResponseFormatJSONObjectParam
	ResponseFormatJSONSchemaParam           = shared.ResponseFormatJSONSchemaParam
	ResponseFormatJSONSchemaJSONSchemaParam = shared.ResponseFormatJSONSchemaJSONSchemaParam
	ResponseFormatTextParam                 = shared.ResponseFormatTextParam
	ResponsesModel                          = shared.ResponsesModel
)

// String, Int, Bool and Float build the optional parameter wrappers, so that
// an explicitly-set zero value is distinguishable from an omitted one.
func String(value string) param.Opt[string]  { return param.NewOpt(value) }
func Int(value int64) param.Opt[int64]       { return param.NewOpt(value) }
func Bool(value bool) param.Opt[bool]        { return param.NewOpt(value) }
func Float(value float64) param.Opt[float64] { return param.NewOpt(value) }

// Client talks to the OpenAI API. Build it with [NewClient].
type Client struct {
	Options    []option.RequestOption
	Chat       ChatService
	Embeddings EmbeddingService
	Models     ModelService
	Responses  responses.ResponseService
}

// defaultResponseHeaderTimeout bounds the wait for response headers. It does
// not cover the body, so streams are unaffected.
const defaultResponseHeaderTimeout = 10 * time.Minute

func defaultHTTPClient() *http.Client {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t = t.Clone()
		t.ResponseHeaderTimeout = defaultResponseHeaderTimeout
		return &http.Client{Transport: t}
	}
	return &http.Client{Transport: http.DefaultTransport}
}

// DefaultClientOptions returns the options read from the environment
// (OPENAI_BASE_URL, OPENAI_API_KEY, OPENAI_CUSTOM_HEADERS).
func DefaultClientOptions() []option.RequestOption {
	defaults := []option.RequestOption{
		option.WithHTTPClient(defaultHTTPClient()),
		option.WithBaseURL("https://api.openai.com/v1/"),
	}
	if o, ok := os.LookupEnv("OPENAI_BASE_URL"); ok {
		defaults = append(defaults, option.WithBaseURL(o))
	}
	if o, ok := os.LookupEnv("OPENAI_API_KEY"); ok {
		defaults = append(defaults, option.WithAPIKey(o))
	}
	if o, ok := os.LookupEnv("OPENAI_CUSTOM_HEADERS"); ok {
		for line := range strings.SplitSeq(o, "\n") {
			if name, value, ok := strings.Cut(line, ":"); ok {
				defaults = append(defaults, option.WithHeader(strings.TrimSpace(name), strings.TrimSpace(value)))
			}
		}
	}
	return defaults
}

// NewClient builds a client whose options apply to every request it makes.
// The environment defaults are applied first, so explicit options win.
func NewClient(opts ...option.RequestOption) (r Client) {
	opts = append(DefaultClientOptions(), opts...)
	return Client{
		Options:    opts,
		Chat:       NewChatService(opts...),
		Embeddings: NewEmbeddingService(opts...),
		Models:     NewModelService(opts...),
		Responses:  responses.NewResponseService(opts...),
	}
}
