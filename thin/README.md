# `thin/` — hand-written replacements for the generated LLM SDKs

**Experiment, not a merge candidate.** This branch exists to measure how much of the
`docker-agent` binary is attributable to the two code-generated LLM SDKs, by replacing them with
hand-written clients that cover only the API surface this repository actually uses.

## Result

`165.32 MB → 123.82 MB` (**−41.50 MB, −25.1%**) with **zero application-code changes**.

| Build | real SDKs | thin clients | delta |
|---|---:|---:|---:|
| default (`task build`) | 165.32 MB | 123.82 MB | **−41.50 MB (−25.1%)** |
| stripped (`-s -w`) | 116.71 MB | 88.48 MB | −28.23 MB (−24.2%) |
| stripped + thin vs. today's shipping binary | 165.32 MB | 88.48 MB | −76.84 MB (−46.5%) |

| Package | before | after |
|---|---:|---:|
| `github.com/openai/openai-go/v3` | 19.00 MB | 0.23 MB |
| `github.com/anthropics/anthropic-sdk-go` | 12.27 MB | 0.25 MB |

`.gopclntab` shrinks by 12.9 MB — more than `.text` (10.2 MB). That is the generated-SDK tax:
pclntab scales with *function count*, and the two SDKs contributed roughly 75,000 of the binary's
~197,000 functions, most of them compiler-emitted wrappers over thousands of tiny param structs.

| | hand-written | upstream (non-test) |
|---|---:|---:|
| `thin/anthropic-sdk-go` | 3,119 lines / 14 files | 93,158 lines |
| `thin/openai-go` | 2,915 lines / 16 files | 138,973 lines |

## How it is wired

Each directory is a separate Go module whose `module` path is the upstream import path, selected
through a `replace` in the root `go.mod`:

```
replace github.com/anthropics/anthropic-sdk-go => ./thin/anthropic-sdk-go
replace github.com/openai/openai-go/v3         => ./thin/openai-go
```

Nothing under `pkg/`, `cmd/`, `e2e/`, `examples/` or `main.go` is modified. That constraint is what
makes the measurement trustworthy: the compiler enumerates the exact API surface the repository
uses, and the existing unmodified test suites are the conformance suite.

Drop the two `replace` lines to build against the real SDKs again.

The size reduction comes from using stdlib `encoding/json` with the `omitzero` tag (Go 1.24+)
instead of the SDKs' reflective `apijson`/`param` encoders: request unions are structs of `OfXxx`
pointers with a hand-written `MarshalJSON`, response unions are flattened structs with `AsAny()`.

## Evidence of behavioural equivalence

- `go test ./...` — zero new failures. The remaining failures are pre-existing and were re-verified
  against the real SDKs in the same sandbox: `pkg/rag/treesitter` (needs `CGO_ENABLED=1`) and
  `TestFileCache_dedupSkipsRedundantWrite` (fails with the real SDKs too).
- `go test ./e2e/...` passes, including the go-vcr cassette suites. Their matcher compares the
  **exact request body string** against traffic recorded with the real SDKs, so this is byte-level
  wire equivalence for the recorded traffic. It caught two real fidelity bugs during development:
  key order when injecting `"stream":true` (upstream appends via `sjson`), and HTML-escaping of
  `<`/`>` in request bodies.
- `golangci-lint run` reports the same findings as the baseline, plus two expected
  `gomoddirectives` "local replacement are not allowed" for the `replace` lines.

## Scope — what these clients deliberately do not cover

Implemented: Messages + Beta Messages + CountTokens (Anthropic); chat/completions, responses,
embeddings, models (OpenAI). Absent: batches, files, fine-tuning, vector stores, assistants, audio,
images, realtime, webhooks, Bedrock, MCP, auto-pagination, stream accumulators. Response structs
carry only the fields this repository reads and drop unknown JSON fields; `respjson` presence
metadata exists only on the four response types the code inspects. Anthropic workload-identity
federation and Vertex are re-implemented but are the least-validated areas, since the repo's tests
cover only their validation paths and not their wire path.

See `thin/anthropic-sdk-go/README.md` for the per-item list on the Anthropic side.

Adopting this would mean owning ~6,000 lines of client code and re-adding surface as new provider
features land. That is the trade to weigh against 41.5 MB.

## Cheaper win, no ownership cost

`-ldflags "-s -w"` in `scripts/build.sh` for release builds strips 48.6 MB of DWARF and symbol
tables on its own. Combined with this experiment: 165 MB → 88 MB.
