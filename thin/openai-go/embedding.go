package openai

import (
	"context"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3/internal/encjson"
	"github.com/openai/openai-go/v3/internal/requestconfig"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared/constant"
)

// EmbeddingService talks to POST /embeddings.
type EmbeddingService struct {
	Options []option.RequestOption
}

func NewEmbeddingService(opts ...option.RequestOption) EmbeddingService {
	return EmbeddingService{Options: opts}
}

// New creates embeddings for one or more inputs.
func (r EmbeddingService) New(ctx context.Context, body EmbeddingNewParams, opts ...option.RequestOption) (*CreateEmbeddingResponse, error) {
	encoded, err := encjson.Marshal(body)
	if err != nil {
		return nil, err
	}
	res := &CreateEmbeddingResponse{}
	err = requestconfig.ExecuteNewRequest(ctx, http.MethodPost, "embeddings", encoded, res,
		append(r.Options[:len(r.Options):len(r.Options)], opts...)...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// EmbeddingNewParams is the body of an embeddings request.
type EmbeddingNewParams struct {
	Input          EmbeddingNewParamsInputUnion `json:"input,omitzero"`
	Model          string                       `json:"model,omitzero"`
	Dimensions     param.Opt[int64]             `json:"dimensions,omitzero"`
	User           param.Opt[string]            `json:"user,omitzero"`
	EncodingFormat string                       `json:"encoding_format,omitzero"`
}

// EmbeddingNewParamsInputUnion is the text (or texts) to embed.
type EmbeddingNewParamsInputUnion struct {
	OfString             param.Opt[string]
	OfArrayOfStrings     []string
	OfArrayOfTokens      []int64
	OfArrayOfTokenArrays [][]int64
}

func (u EmbeddingNewParamsInputUnion) IsZero() bool {
	return u.OfString.IsZero() && u.OfArrayOfStrings == nil && u.OfArrayOfTokens == nil && u.OfArrayOfTokenArrays == nil
}

func (u EmbeddingNewParamsInputUnion) MarshalJSON() ([]byte, error) {
	switch {
	case u.OfString.Valid():
		return encjson.Marshal(u.OfString)
	case u.OfArrayOfStrings != nil:
		return encjson.Marshal(u.OfArrayOfStrings)
	case u.OfArrayOfTokens != nil:
		return encjson.Marshal(u.OfArrayOfTokens)
	case u.OfArrayOfTokenArrays != nil:
		return encjson.Marshal(u.OfArrayOfTokenArrays)
	}
	return []byte("null"), nil
}

// CreateEmbeddingResponse is the embeddings response.
type CreateEmbeddingResponse struct {
	Data   []Embedding                  `json:"data"`
	Model  string                       `json:"model"`
	Object constant.List                `json:"object"`
	Usage  CreateEmbeddingResponseUsage `json:"usage"`
}

type Embedding struct {
	Embedding []float64          `json:"embedding"`
	Index     int64              `json:"index"`
	Object    constant.Embedding `json:"object"`
}

type CreateEmbeddingResponseUsage struct {
	PromptTokens int64 `json:"prompt_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// ModelService talks to GET /models.
type ModelService struct {
	Options []option.RequestOption
}

func NewModelService(opts ...option.RequestOption) ModelService {
	return ModelService{Options: opts}
}

// Model describes one model exposed by the endpoint.
type Model struct {
	ID           string         `json:"id"`
	Created      int64          `json:"created"`
	Object       constant.Model `json:"object"`
	OwnedBy      string         `json:"owned_by"`
	ShutdownDate time.Time      `json:"shutdown_date,omitzero"`
}

// ModelPage is a page of models. The endpoint is not paginated in practice:
// every model is returned at once.
type ModelPage struct {
	Data   []Model       `json:"data"`
	Object constant.List `json:"object"`
}

// List returns the models the endpoint exposes.
func (r ModelService) List(ctx context.Context, opts ...option.RequestOption) (*ModelPage, error) {
	res := &ModelPage{}
	err := requestconfig.ExecuteNewRequest(ctx, http.MethodGet, "models", nil, res,
		append(r.Options[:len(r.Options):len(r.Options)], opts...)...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Get returns one model by id.
func (r ModelService) Get(ctx context.Context, model string, opts ...option.RequestOption) (*Model, error) {
	res := &Model{}
	err := requestconfig.ExecuteNewRequest(ctx, http.MethodGet, "models/"+model, nil, res,
		append(r.Options[:len(r.Options):len(r.Options)], opts...)...)
	if err != nil {
		return nil, err
	}
	return res, nil
}
