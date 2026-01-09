### DMR (Docker Model Runner) provider options

When using the `dmr` provider, you can use the `provider_opts` key for DMR
runtime-specific (e.g. llama.cpp/vllm) options and speculative decoding:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    provider_opts:
      # general flags passed to the underlying model runtime
      runtime_flags: ["--ngl=33", "--repeat-penalty=1.2", ...] # or comma/space-separated string
      # speculative decoding for faster inference
      speculative_draft_model: ai/qwen3:1B
      speculative_num_tokens: 5
      speculative_acceptance_rate: 0.8
```

The default base_url Docker `cagent` will use for DMR providers is
`http://localhost:12434/engines/llama.cpp/v1`. DMR itself might need to be
enabled via [Docker Desktop's
settings](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-desktop)
on macOS and Windows, and via the command-line on [Docker CE on
Linux](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-engine).

See the [DMR Provider
documentation](docs/usage.md#dmr-docker-model-runner-provider-usage) for more
details on runtime flags and speculative decoding options.
