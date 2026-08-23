# thin `anthropic-sdk-go`

A hand-written, drop-in replacement for the parts of
`github.com/anthropics/anthropic-sdk-go` v1.66.0 that docker-agent actually
uses. Wired in from the repo root with:

```
go mod edit -replace github.com/anthropics/anthropic-sdk-go=./thin/anthropic-sdk-go
```

No application code changes: `go build ./...`, `go vet ./...` and the existing
`pkg/model/provider/anthropic/...` suite (204 tests, ~2,700 lines, asserting on
exact JSON wire payloads) pass unmodified against it.

## What is covered

* Messages API — `POST /v1/messages` (SSE streaming) and
  `POST /v1/messages/count_tokens`.
* Beta Messages API — the same two endpoints with `?beta=true` and one
  `anthropic-beta` header per entry of `params.Betas`.
* Request params: messages, system blocks, tools (incl. `defer_loading` and
  `cache_control`), thinking (token budget / adaptive), `output_config`
  (effort, JSON-schema format, extra fields such as `task_budget`),
  temperature / top_p / top_k, content blocks (text, image base64+URL,
  document, thinking, redacted thinking, tool_use, tool_result, tool_reference).
* Response streams: the flattened event unions with `AsAny()`, the delta and
  content-block variants, and usage (incl. `output_tokens_details`).
* `option`: `WithAPIKey`, `WithAuthToken`, `WithBaseURL`, `WithHTTPClient`,
  `WithHeader`/`Add`/`Del`, `WithQuery`, `WithMaxRetries`, `WithRequestTimeout`,
  `WithMiddleware`, `WithJSONSet`/`WithJSONDel`, `WithFederationTokenProvider`.
* `packages/param`: `Opt[T]` with `omitzero` semantics.
* `packages/ssestream`: `Decoder`, `Event`, `Stream[T]`.
* `shared`: `ErrorType` constants and `ErrorObjectUnion`.
* `vertex`: base URL per region, OAuth2 credentials, and the middleware that
  rewrites `/v1/messages` into `:rawPredict` / `:streamRawPredict` and injects
  `anthropic_version`.
* `*anthropic.Error` for non-2xx responses and for in-band SSE `error` events,
  carrying `StatusCode`, `RequestID`, `Type()` and `RawJSON()`, and formatting
  its message exactly like upstream.
* Retry policy: 2 retries by default, on 408/409/429/5xx and connection
  errors, honoring `Retry-After` / `Retry-After-Ms` / `x-should-retry`, with
  the same exponential backoff and jitter.

## What is deliberately not covered

Everything else the generated SDK ships: message batches, files, models,
skills, agents, sessions, tunnels, vaults, webhooks, memory stores, the legacy
completions API, Bedrock, MCP and tool runners, prompt-caching helpers beyond
`cache_control`, and every content-block kind docker-agent never builds
(search results, web search/fetch, code execution, containers, computer and
browser toolsets, citations, MCP tool results).

Known behavioural simplifications, relative to upstream:

* `WithJSONSet` re-serializes the body from a map, so keys it touches are
  emitted in a different order (the JSON is equivalent; upstream appends via
  `sjson`). Paths are dot-separated keys only — no array indices.
* `BetaJSONSchemaOutputFormat` passes the schema through unchanged; upstream
  normalizes unsupported JSON-schema keywords into descriptions.
* Response structs carry only the fields docker-agent reads, and none of the
  `JSON`/`respjson.Field` presence metadata; `RawJSON()` is available on the
  unions. Unknown JSON fields are dropped rather than kept in `ExtraFields`.
* `NewClient` reads credentials from the environment only — no profile or
  shared-config-file discovery, and no `WithoutEnvironmentDefaults`.
* Workload identity federation performs the same `jwt-bearer` exchange against
  `<base>/v1/oauth/token` with in-memory caching and a 120s refresh window, but
  has no shared cross-client token cache, no typed `OAuthTokenError` with
  operator hints, and no `ANTHROPIC_*` federation env-var discovery.
* `vertex` authorizes with an `oauth2` token source instead of
  `google.golang.org/api/transport`, so Google-specific transport features
  (quota project, ALTS, custom endpoints) do not apply.
* Requests omit the `X-Stainless-*` telemetry headers, and the `User-Agent` is
  `Anthropic/Go` without a version.
