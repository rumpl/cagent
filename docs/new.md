## Quickly generate agents and agent teams with `cagent new`

Using the command `cagent new` you can quickly generate agents or multi-agent
teams using a single prompt!  
`cagent` has a built-in agent dedicated to this task.

To use the feature, you must have an Anthropic, OpenAI or Google API key
available in your environment or specify a local model to run with DMR (Docker
Model Runner).

You can choose what provider and model gets used by passing the `--model
provider/modelname` flag to `cagent new`

If `--model` is unspecified, `cagent new` will automatically choose between
these three providers in order based on the first api key it finds in your
environment.

```sh
export ANTHROPIC_API_KEY=your_api_key_here  # first choice. default model claude-sonnet-4-0
export OPENAI_API_KEY=your_api_key_here     # if anthropic key not set. default model gpt-5-mini
export GOOGLE_API_KEY=your_api_key_here     # if anthropic and openai keys are not set. default model gemini-2.5-flash
```

`--max-tokens` can be specified to override the context limit used.  
When using DMR, the default is 16k to limit memory usage. With all other
providers the default is 64k

`--max-iterations` can be specified to override how many times the agent is
allowed to loop when doing tool calling etc. When using DMR, the default is set
to 20 (small local models have the highest chance of getting confused and
looping endlessly). For all other providers, the default is 0 (unlimited).

Example of provider, model, context size and max iterations overriding:

```sh
# Use GPT-5 via OpenAI
cagent new --model openai/gpt-5

# Use a local model (ai/gemma3-qat:12B) via DMR
cagent new --model dmr/ai/gemma3-qat:12B

# Override the max_tokens used during generation, default is 64k, 16k when using the dmr provider
cagent new --model openai/gpt-5-mini --max-tokens 32000

# Override max_iterations to limit how much the model can loop autonomously when tool calling
cagent new --model dmr/ai/gemma3n:2B-F16 --max-iterations 15
```
