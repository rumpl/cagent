## RAG (Retrieval-Augmented Generation)

Give your agents access to document knowledge bases using cagent's modular RAG system. It supports:

- Background indexing of files;
- Re-indexing on file changes;
- Using one or more retrieval strategies that can be used individually or combined for hybrid search.

### RAG Configuration

Configure RAG sources at the top level of your config, then reference them in agents:

```yaml
rag:
  my_docs:
    description: "Technical documentation knowledge base"
    docs: [./documents, ./some-doc.md]
    strategies:
      - type: chunked-embeddings
        model: openai/text-embedding-3-small
        database: ./docs.db
        vector_dimensions: 1536

agents:
  root:
    model: openai/gpt-4o
    instruction: |
      You are an assistant with access to an internal knowledge base.
      Use the knowledge base to gather context before answering user questions
    rag: [my_docs] # Reference the RAG source
```

### Retrieval Strategies

#### Chunked-Embeddings Strategy (Semantic Search)

Uses embedding models for semantic similarity:

```yaml
rag:
  semantic_search:
    docs: [./knowledge_base]
    strategies:
      - type: chunked-embeddings
        model: openai/text-embedding-3-small
        database: ./vector.db
        vector_dimensions: 1536
        similarity_metric: cosine_similarity
        threshold: 0.5
        limit: 10
        batch_size: 50
        max_embedding_concurrency: 3
        chunking:
          size: 1000
          overlap: 100
```

**Best for:** Understanding intent, synonyms, paraphrasing, multilingual queries

#### Semantic-Embeddings Strategy (LLM-Enhanced Semantic Search)

Uses an LLM to generate semantic summaries of each chunk before embedding, capturing meaning and intent rather than raw text:

```yaml
rag:
  code_search:
    docs: [./src, ./pkg]
    strategies:
      - type: semantic-embeddings
        embedding_model: openai/text-embedding-3-small
        vector_dimensions: 1536
        chat_model: openai/gpt-4o-mini # LLM for generating summaries
        database: ./semantic.db
        threshold: 0.3
        limit: 10
        ast_context: true # Include AST metadata in prompts
        chunking:
          size: 1000
          code_aware: true # AST-aware chunking for best results
```

**Best for:** Code search, understanding intent, finding implementations by what they do rather than exact names

**Trade-offs:** Higher quality retrieval but slower indexing (LLM call per chunk) and additional API costs

**Parameters:**

- `embedding_model` (required): Embedding model for vector similarity
- `chat_model` (required): Chat model to generate semantic summaries
- `vector_dimensions` (required): Embedding vector dimensions
- `semantic_prompt`: Custom prompt template (uses `${path}`, `${content}`, `${ast_context}` placeholders)
- `ast_context`: Include TreeSitter AST metadata in prompts (default: `false`)

#### BM25 Strategy (Keyword Search)

Uses traditional keyword matching:

```yaml
rag:
  keyword_search:
    docs: [./knowledge_base]
    strategies:
      - type: bm25
        database: ./bm25.db
        k1: 1.5 # Term frequency saturation
        b: 0.75 # Length normalization
        threshold: 0.3 # Minimum BM25 score
        limit: 10
        chunking:
          size: 1000
          overlap: 100
```

**Best for:** Exact terms, technical jargon, proper nouns, code

### Hybrid Retrieval

Combine multiple strategies for best results:

```yaml
rag:
  hybrid_search:
    docs: [./shared_docs] # Documents indexed by all strategies

    strategies:
      - type: chunked-embeddings
        model: embedder
        docs: [./docs] # Additional chunked-embeddings-specific docs
        database: ./vector.db
        threshold: 0.5
        limit: 20 # Retrieve 20 candidates
        chunking: { size: 1000, overlap: 100 }

      - type: bm25
        docs: [./code] # Additional BM25-specific docs
        database: ./bm25.db
        k1: 1.5
        b: 0.75
        threshold: 0.3
        limit: 15 # Retrieve 15 candidates
        chunking: { size: 1000, overlap: 100 }

    results:
      fusion:
        strategy: rrf # Reciprocal Rank Fusion
        k: 60
      deduplicate: true
      limit: 5 # Final top 5 results

agents:
  root:
    model: openai/gpt-4o
    rag: [hybrid_search]
```

**Features:**

- **Parallel execution**: Strategies run concurrently (total time = max, not sum)
- **Per-strategy docs**: Different content for different strategies
- **Multi-level limits**: Strategy limits control fusion input, final limit controls output
- **Automatic fusion**: Combines results intelligently

### Fusion Strategies

When using multiple retrieval strategies, choose how to combine results:

#### Reciprocal Rank Fusion (RRF) - Recommended

```yaml
results:
  fusion:
    strategy: rrf
    k: 60 # Smoothing parameter (higher = more uniform ranking)
```

**Best for:** Combining different retrieval methods. Rank-based, doesn't require score normalization.

#### Weighted Fusion

```yaml
results:
  fusion:
    strategy: weighted
    weights:
      chunked-embeddings: 0.7
      bm25: 0.3
```

**Best for:** When you know which strategy performs better for your use case.

#### Max Score Fusion

```yaml
results:
  fusion:
    strategy: max
```

**Best for:** Strategies using the same scoring scale. Takes maximum score.

### Result Reranking

Reranking re-scores retrieved documents using a specialized model to improve relevance. This is applied **after** retrieval and fusion, but **before** the final limit.

#### Why Rerank?

Initial retrieval strategies (embeddings, BM25) are fast but approximate. Reranking uses a more sophisticated model to:

- Improve relevance scoring accuracy
- Apply domain-specific criteria
- Consider document metadata (source, recency, type)
- Filter low-quality results

#### Provider Support

| Provider  | Implementation                       | Recommended Use Case         |
| --------- | ------------------------------------ | ---------------------------- |
| DMR       | Native `/rerank` endpoint            | Production (fast, efficient) |
| OpenAI    | Chat completion + structured outputs | Flexible, criteria-based     |
| Anthropic | Beta API + structured outputs        | Complex relevance rules      |
| Gemini    | Structured outputs                   | Cost-effective scoring       |

#### Basic Reranking Configuration

```yaml
rag:
  docs_with_reranking:
    docs: [./knowledge_base]
    strategies:
      - type: chunked-embeddings
        model: openai/text-embedding-3-small
        limit: 20 # Retrieve more candidates for reranking

    results:
      reranking:
        model: openai/gpt-4o-mini
      limit: 5 # Final results after reranking
```

#### Advanced Reranking Configuration

```yaml
rag:
  advanced_reranking:
    docs: [./documents]
    strategies:
      - type: chunked-embeddings
        model: embedder
        limit: 20

    results:
      reranking:
        model: openai/gpt-4o-mini

        # top_k: Only rerank top K results (optional)
        # Useful for cost optimization when retrieving many documents
        # Set to 0 or omit to rerank all results
        top_k: 10

        # threshold: Minimum relevance score (0.0-1.0) after reranking (default: 0.5)
        # Results below threshold are filtered out
        # Applied before final limit
        threshold: 0.3

        # criteria: Domain-specific relevance guidance (optional)
        # The model receives metadata: source path, chunk index, created_at
        # Use this to guide scoring based on source, recency, or content type
        criteria: |
          When scoring relevance, prioritize:
          - Content from official documentation over blog posts
          - Recent information (check created_at dates)
          - Practical examples and implementation details
          - Documents from docs/ directory when available

      deduplicate: true
      limit: 5
```

#### DMR Native Reranking

DMR offers a native reranking endpoint:

```yaml
models:
  dmr-reranker:
    provider: dmr
    model: hf.co/ggml-org/qwen3-reranker-0.6b-q8_0-gguf # reranking specific model
    # Note: Native reranking doesn't support criteria parameter

rag:
  knowledge_base:
    docs: [./documents]
    strategies:
      - type: chunked-embeddings
        model: embedder
        limit: 20

    results:
      reranking:
        model: dmr-reranker
        threshold: 0.5
      limit: 5
```

#### Reranking Model Configuration

Configure sampling parameters for deterministic or creative scoring. Note that temperature defaults to 0.0 for reranking when not explicitly set:

```yaml
models:
  # Deterministic reranking (default behavior)
  openai-rerank:
    provider: openai
    model: gpt-4o-mini
    # temperature: 0.0  # Default for reranking (explicit setting optional)
    max_tokens: 16384

  # Anthropic with structured outputs
  claude-rerank:
    provider: anthropic
    model: claude-sonnet-4-5 # model needs to support structured outputs
    # temperature: 0.0  # Default for reranking
    max_tokens: 16384

  # Gemini reranking
  gemini-rerank:
    provider: google
    model: gemini-2.5-flash
    # temperature: 0.0  # Default for reranking
    max_tokens: 16384
```

#### Reranking Configuration Reference

| Field       | Type   | Description                                                      | Default |
| ----------- | ------ | ---------------------------------------------------------------- | ------- |
| `model`     | string | Model reference for reranking                                    | -       |
| `top_k`     | int    | Only rerank top K results (0 = rerank all)                       | 0       |
| `threshold` | float  | Minimum score (0.0-1.0) after reranking                          | 0.5     |
| `criteria`  | string | Domain-specific relevance guidance (not supported by DMR native) | ""      |

**Notes:**

- Reranking adds latency but significantly improves result quality
- Use `top_k` to trade quality for speed and cost
- Temperature defaults to 0.0 for deterministic scoring when not explicitly set (OpenAI, Anthropic, Gemini)
- Default threshold of 0.5 filters documents with negative logits (sigmoid < 0.5 = not relevant)
- DMR native reranking is fastest but doesn't support custom criteria
- Criteria works with OpenAI, Anthropic, and Gemini (chat-based reranking using structured-outputs)
- Fallback: If reranking fails, original post-fusion retrieval scores are used

### RAG Configuration Reference

| Field         | Type     | Description                                                           |
| ------------- | -------- | --------------------------------------------------------------------- |
| `docs`        | []string | Document paths/directories (shared across strategies)                 |
| `description` | string   | Human-readable description                                            |
| `respect_vcs` | boolean  | Whether to respect VCS ignore files like .gitignore (default: `true`) |
| `strategies`  | []object | Array of strategy configurations                                      |
| `results`     | object   | Post-processing configuration                                         |

**Strategy Configuration:**

- `type`: Strategy type (`chunked-embeddings`, `semantic-embeddings`, `bm25`)
- `docs`: Strategy-specific documents (optional, augments shared docs)
- `database`: Database configuration (path to local sqlite db)
- `chunking`: Chunking configuration
- `limit`: Max results from this strategy (for fusion)
- `respect_vcs`: Override RAG-level VCS ignore setting for this strategy only (optional)

**Chunked-Embeddings Strategy Parameters:**

- `model` (required): Embedding model reference
- `database`: Database configuration
  - simple string path to local sqlite db (e.g. `./vector.db`)
- `similarity_metric`: Similarity metric (only `cosine_similarity` now, `euclidean_distance`, `dot_product`, etc to come in the future)
- `threshold`: Minimum similarity (0–1, default: `0.5`)
- `limit`: Max candidates from this strategy for fusion input (default: `5`)
- `batch_size`: Number of chunks per embedding request (default: `50`)
- `max_embedding_concurrency`: Maximum concurrent embedding batch requests (default: `3`)
- `chunking.size`: Chunk size in characters (default: `1000`)
- `chunking.overlap`: Overlap between chunks (default: `75`)

**Semantic-Embeddings Strategy:**

- `embedding_model` (required): Embedding model reference (e.g., `openai/text-embedding-3-small`)
- `chat_model` (required): Chat model for generating semantic summaries (e.g., `openai/gpt-4o-mini`)
- `vector_dimensions` (required): Embedding vector dimensions (e.g., `1536` for text-embedding-3-small)
- `database`: Database configuration (same formats as chunked-embeddings)
- `semantic_prompt`: Custom prompt template using JS template literals (`${path}`, `${basename}`, `${chunk_index}`, `${content}`, `${ast_context}`)
- `ast_context`: Include TreeSitter AST metadata in semantic prompts. Useful for code (default: `false`, best with `code_aware` chunking)
- `similarity_metric`: Similarity metric (default: `cosine_similarity`)
- `threshold`: Minimum similarity (0–1, default: `0.5`)
- `limit`: Max candidates from this strategy for fusion input (default: `5`)
- `embedding_batch_size`: Chunks per embedding request (default: `50`)
- `max_embedding_concurrency`: Concurrent embedding/LLM requests (default: `3`)
- `max_indexing_concurrency`: Concurrent file indexing (default: `3`)
- `chunking.size`: Chunk size in characters (default: `1000`)
- `chunking.overlap`: Overlap between chunks (default: `75`)
- `chunking.code_aware`: Use AST-based chunking (default: `false`, if `true` the `chunking.overlap` will be ignored)

**BM25 Strategy:**

- `database`: Database configuration (same formats as chunked-embeddings)
- `k1`: Term frequency saturation (recommended range: `1.2–2.0`, default: `1.5`)
- `b`: Length normalization (`0–1`, default: `0.75`)
- `threshold`: Minimum BM25 score (default: `0.0`)
- `limit`: Max candidates from this strategy for fusion input (default: `5`)
- `chunking.size`: Chunk size in characters (default: `1000`)
- `chunking.overlap`: Overlap between chunks (default: `75`)

**Code-Aware Chunking:**

When indexing source code, you can enable code-aware chunking to produce semantically aligned chunks based on the code's AST (Abstract Syntax Tree). This keeps functions and methods intact rather than splitting them arbitrarily:

```yaml
rag:
  codebase:
    docs: [./src]
    strategies:
      - type: bm25
        database: ./code.db
        chunking:
          size: 2000
          code_aware: true # Enable AST-based chunking
```

- `chunking.code_aware`: When `true`, uses tree-sitter for AST-based chunking (default: `false`), and `size` becomes indicative

**Notes:**

- Currently supports **Go** source files (`.go`). More languages will be added incrementally.
- Falls back to plain text chunking for unsupported file types.
- Produces chunks that align with code structure (functions, methods, type declarations).
- Particularly useful for code search and retrieval tasks.

**Results:**

- `limit`: Final number of results (default: `15`)
- `deduplicate`: Remove duplicates (default: `true`)
- `fusion.strategy`: rrf, weighted, or max
- `fusion.k`: RRF parameter
- `fusion.weights`: Weights for weighted fusion
- `reranking`: Optional reranking configuration (see [Result Reranking](#result-reranking) section)
  - `reranking.model`: Model reference for reranking
  - `reranking.top_k`: Only rerank top K results (default: `0` = rerank all)
  - `reranking.threshold`: Minimum score after reranking (default: `0.0`)
  - `reranking.criteria`: Domain-specific relevance guidance (optional, not supported by DMR native)
- `return_full_content`: When `true`, return full document contents instead of just matched chunks (default: `false`)

### Debugging RAG

Enable debug logging to see detailed retrieval and fusion information:

```bash
./bin/cagent run config.yaml --debug --log-file cagent.debug
```

Look for logs tagged with:

- `[RAG Manager]` - Overall operations
- `[Chunked-Embeddings Strategy]` - Chunked-embeddings retrieval
- `[BM25 Strategy]` - BM25 retrieval
- `[RRF Fusion]` / `[Weighted Fusion]` / `[Max Fusion]` - Result fusion
- `[Reranker]` - Reranking operations and score adjustments

### RAG Examples

See `examples/rag/` directory:

- `examples/rag/bm25.yaml` - BM25 strategy only
- `examples/rag/hybrid.yaml` - Hybrid retrieval (chunked-embeddings + BM25)
- `examples/rag/semantic_embeddings.yaml` - Semantic-embeddings strategy with LLM summaries
- `examples/rag/reranking.yaml` - Reranking with various providers
- `examples/rag/reranking_full_example.yaml` - Complete reranking configuration reference

## Examples

### Development Team

A complete development team with specialized roles:

```yaml
agents:
  root:
    model: claude
    description: Technical lead coordinating development
    instruction: |
      You are a technical lead managing a development team.
      Coordinate tasks between developers and ensure quality.
    sub_agents: [developer, reviewer, tester]

  developer:
    model: claude
    description: Expert software developer
    instruction: |
      You are an expert developer. Write clean, efficient code
      and follow best practices.
    toolsets:
      - type: filesystem
      - type: shell
      - type: think

  reviewer:
    model: gpt4
    description: Code review specialist
    instruction: |
      You are a code review expert. Focus on code quality,
      security, and maintainability.
    toolsets:
      - type: filesystem

  tester:
    model: gpt4
    description: Quality assurance engineer
    instruction: |
      You are a QA engineer. Write tests and ensure
      software quality.
    toolsets:
      - type: shell
      - type: todo

models:
  gpt4:
    provider: openai
    model: gpt-4o

  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```

### Research Assistant

A research-focused agent with web access:

```yaml
agents:
  root:
    model: claude
    description: Research assistant with web access
    instruction: |
      You are a research assistant. Help users find information,
      analyze data, and provide insights.
    toolsets:
      - type: mcp
        command: mcp-web-search
        args: ["--provider", "duckduckgo"]
      - type: todo
      - type: memory
        path: "./research_memory.db"

models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```
