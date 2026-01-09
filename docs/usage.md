# Usage and configuration

This guide will help you get started with `cagent` and learn how to use its
powerful multi-agent system to accomplish various tasks.

## What is cagent?

`cagent` is a powerful, customizable multi-agent system that orchestrates AI
agents with specialized capabilities and tools. It features:

- **🏗️ Multi-tenant architecture** with client isolation and session management
- **🔧 Rich tool ecosystem** via Model Context Protocol (MCP) integration
- **🤖 Hierarchical agent system** with intelligent task delegation
- **🌐 Multiple interfaces** including CLI, TUI and API server
- **📦 Agent distribution** via Docker registry integration
- **🔒 Security-first design** with proper client scoping and resource isolation
- **⚡ Event-driven streaming** for real-time interactions
- **🧠 Multi-model support** (OpenAI, Anthropic, Gemini, [AWS
  Bedrock](https://aws.amazon.com/bedrock/), [Docker Model Runner
  (DMR)](https://docs.docker.com/ai/model-runner/))

## Why?

After passing the last year+ building AI agents of various types, using a
variety of software solutions and frameworks, we kept asking ourselves some of
the same questions:

- How can we make building and running useful agentic systems less of a hassle?
- Most agents we build end up using many of the same building blocks. Can we
  re-use most of those building block and have declarative configurations for
  new agents?
- How can we package and share agents amongst each other as simply as possible
  without all the headaches?

We really think we're getting somewhere as we build out the primitives of
`cagent` so, in keeping with our love for open-source software in general, we
decided to **share it and build it in the open** to allow the community at large
to make use of our work and contribute to the future of the project itself.

## Running Agents

### Command Line Interface

cagent provides multiple interfaces and deployment modes:

```bash
# Terminal UI (TUI)
$ cagent run config.yaml
$ cagent run config.yaml -a agent_name    # Run a specific agent
$ cagent run config.yaml --debug          # Enable debug logging
$ cagent run config.yaml --yolo           # Auto-accept all the tool calls
$ cagent run config.yaml "First message"  # Start the conversation with the agent with a first message
$ cagent run config.yaml -c df            # Run with a named command from YAML

# Model Override Examples
$ cagent run config.yaml --model anthropic/claude-sonnet-4-0    # Override all agents to use Claude
$ cagent run config.yaml --model "agent1=openai/gpt-4o"         # Override specific agent
$ cagent run config.yaml --model "agent1=openai/gpt-4o,agent2=anthropic/claude-sonnet-4-0"  # Multiple overrides

# One off without TUI
$ cagent exec config.yaml                 # Run the agent once, with default instructions
$ cagent exec config.yaml "First message" # Run the agent once with instructions
$ cagent exec config.yaml --yolo          # Run the agent once and auto-accept all the tool calls

# API Server (HTTP REST API)
$ cagent api config.yaml
$ cagent api config.yaml --listen :8080
$ cagent api ociReference # start API from oci reference
## start API from oci reference, auto-pull every 10 mins and reload if a new team was pulled
$ cagent api ociReference --pull-interval 10

# ACP Server (Agent Client Protocol via stdio)
$ cagent acp config.yaml                 # Start ACP server on stdio

# Other commands
$ cagent new                          # Initialize new project
$ cagent new --model openai/gpt-5-mini --max-tokens 32000  # Override max tokens during generation
$ cagent eval config.yaml             # Run evaluations
$ cagent pull docker.io/user/agent    # Pull agent from registry
$ cagent push docker.io/user/agent    # Push agent to registry
```

#### Default agent

cagent handles a special case for a **default** agent. Running `cagent run` or
`cagent run default` will quickly open the TUI on a default, basic agent. This
agent is capable and has a few tools but is not a replacement for a more
specialized agent. It's here for when you don't think you need a special agent.

#### Aliases

When using the same agent over and over again, from different working
directories, it can be cumbersome to run `cagent run /full/path/to/the/agent`.

That's where `aliases` come in handy. You can define has many short aliases as
you want to full agent file paths of names.

```bash
cagent alias ls           # List the aliases

cagent alias add pirate /full/path/to/the/pirate.yaml
cagent run pirate

cagent alias add other ociReference
cagent run other
```

It's even possible to replace the `default` agent with `aliases`:

```bash
cagent alias add default /full/path/to/the/pirate.yaml
cagent run                # Runs the pirate.yaml agent
```

### Interface-Specific Features

#### File Attachments

In the TUI, you can attach file contents to your message using the `@` trigger:

1. Type `@` to open the file completion menu
2. Start typing to filter files (respects `.gitignore`)
3. Select a file to insert the reference (e.g., `@src/main.go`)
4. When you send your message, the file contents are automatically expanded and
   attached at the end of your message, while `@somefile.txt` references stay in
   your message so the LLM can reference the file contents in the context of
   your question

**Example:**

```
Explain what the code in @pkg/agent/agent.go does
```

The agent gets the full file contents and places them in a structured
`<attachments>` block at the end of the message, while the UI doesn't display
full file contents.

#### TUI Interactive Commands

During TUI sessions, you can use special slash commands. Type `/` to see all
available commands or use the command palette (Ctrl+K):

| Command     | Description                                                                              |
| ----------- | ---------------------------------------------------------------------------------------- |
| `/attach`   | Attach a file to your message (usage: /attach [path])                                    |
| `/compact`  | Summarize the current conversation (usage: /compact [instructions])                      |
| `/copy`     | Copy the current conversation to the clipboard                                           |
| `/cost`     | Show detailed cost breakdown for this session                                            |
| `/eval`     | Create an evaluation report (usage: /eval [filename])                                    |
| `/exit`     | Exit the application                                                                     |
| `/export`   | Export the session as HTML (usage: /export [filename])                                   |
| `/model`    | Change the model for the current agent (see [Model Switching](#runtime-model-switching)) |
| `/new`      | Start a new conversation                                                                 |
| `/sessions` | Browse and load past sessions                                                            |
| `/shell`    | Start a shell                                                                            |
| `/star`     | Toggle star on current session                                                           |
| `/yolo`     | Toggle automatic approval of tool calls                                                  |

#### Runtime Model Switching

The `/model` command (or `ctrl+m`) allows you to change the AI model used by the
current agent during a session. This is useful when you want to:

- Switch to a more capable model for complex tasks
- Use a faster/cheaper model for simple queries
- Test different models without modifying your YAML configuration

**How it works:**

1. Type `/model`, `Ctrl+M` or use the command palette (`Ctrl+K`) and select
   "Model"
2. A picker dialog opens showing:
   - **Config models**: All models defined in your YAML configuration, with the
     agent's default model marked as "(default)"
   - **Custom input**: Type any model in `provider/model` format  
     (e.g., `openai/gpt-5`, `anthropic/claude-sonnet-4-0`)  
     Alloy models are supported with comma separated definitions (e.g.
     `provider1/model1,provider2/model2,...`)
3. Select a model or type a custom one and press Enter

**Persistence:** Your model choice is saved with the session. When you reload a
past session using `/sessions`, the model you selected will automatically be
restored.

To revert to the agent's default model, select the model marked with "(default)"
in the picker.

## 🔧 Configuration Reference

### Agent Properties

| Property               | Type         | Description                                                     | Required |
| ---------------------- | ------------ | --------------------------------------------------------------- | -------- |
| `name`                 | string       | Agent identifier                                                | ✓        |
| `model`                | string       | Model reference                                                 | ✓        |
| `description`          | string       | Agent purpose                                                   | ✓        |
| `instruction`          | string       | Detailed behavior instructions                                  | ✓        |
| `sub_agents`           | array        | List of sub-agent names                                         | ✗        |
| `toolsets`             | array        | Available tools                                                 | ✗        |
| `add_date`             | boolean      | Add current date to context                                     | ✗        |
| `add_environment_info` | boolean      | Add information about the environment (working dir, OS, git...) | ✗        |
| `max_iterations`       | int          | Specifies how many times the agent can loop when using tools    | ✗        |
| `commands`             | object/array | Named prompts for /commands                                     | ✗        |

#### Example

```yaml
agents:
  agent_name:
    model: string # Model reference
    description: string # Agent purpose
    instruction: string # Detailed behavior instructions
    tools: [] # Available tools (optional)
    sub_agents: [] # Sub-agent names (optional)
    add_date: boolean # Add current date to context (optional)
    add_environment_info: boolean # Add information about the environment (working dir, OS, git...) (optional)
    max_iterations: int # How many times this agent can loop when calling tools (optional, default = unlimited)
    commands: # Either mapping or list of singleton maps
      df: "check how much free space i have on my disk"
      ls: "list the files in the current directory"
      greet: "Say hello to ${env.USER} and ask how their day is going"
      analyze: "Analyze the project named ${env.PROJECT_NAME || 'demo'} in the ${env.ENVIRONMENT || 'stage'} environment"
```

### Running named commands

```bash
cagent run ./agent.yaml /df
cagent run ./agent.yaml /ls

export USER=alice
cagent run ./agent.yaml /greet

export PROJECT_NAME=myproject
export ENVIRONMENT=production
cagent run ./agent.yaml /analyze
```

- Commands are evaluated as Javascript Template literals.
- During evaluation, the `env` object contains the user's environment.
- Undefined environment variables expand to empty strings (no error is thrown).

### Model Properties

| Property            | Type       | Description                                                                  | Required |
| ------------------- | ---------- | ---------------------------------------------------------------------------- | -------- |
| `provider`          | string     | Provider: `openai`, `anthropic`, `google`, `amazon-bedrock`, `dmr`           | ✓        |
| `model`             | string     | Model name (e.g., `gpt-4o`, `claude-sonnet-4-0`, `gemini-2.5-flash`)         | ✓        |
| `temperature`       | float      | Randomness (0.0-1.0)                                                         | ✗        |
| `max_tokens`        | integer    | Response length limit                                                        | ✗        |
| `top_p`             | float      | Nucleus sampling (0.0-1.0)                                                   | ✗        |
| `frequency_penalty` | float      | Repetition penalty (0.0-2.0)                                                 | ✗        |
| `presence_penalty`  | float      | Topic repetition penalty (0.0-2.0)                                           | ✗        |
| `base_url`          | string     | Custom API endpoint                                                          | ✗        |
| `thinking_budget`   | string/int | Reasoning effort — OpenAI: effort string, Anthropic/Google: token budget int | ✗        |

#### Example

```yaml
models:
  model_name:
    provider: string # Provider: openai, anthropic, google, amazon-bedrock, dmr
    model: string # Model name: gpt-4o, claude-3-7-sonnet-latest, gemini-2.5-flash, qwen3:4B, ...
    temperature: float # Randomness (0.0-1.0)
    max_tokens: integer # Response length limit
    top_p: float # Nucleus sampling (0.0-1.0)
    frequency_penalty: float # Repetition penalty (0.0-2.0)
    presence_penalty: float # Topic repetition penalty (0.0-2.0)
    parallel_tool_calls: boolean
    thinking_budget: string|integer # OpenAI: effort level string; Anthropic/Google: integer token budget
```

### Reasoning Effort (thinking_budget)

Determine how much the model should think by setting the `thinking_budget`

- **OpenAI**: use effort levels — `minimal`, `low`, `medium`, `high`
- **Anthropic**: set an integer token budget. Range is 1024–32768; must be
  strictly less than `max_tokens`.
- **Google (Gemini)**: set an integer token budget. `0` -> disable thinking,
  `-1` -> dynamic thinking (model decides). Most models: 0–24576 tokens. Gemini
  2.5 Pro: 128–32768 tokens (and cannot disable thinking).

Examples (OpenAI):

```yaml
models:
  gpt:
    provider: openai
    model: gpt-5-mini
    thinking_budget: low

agents:
  root:
    model: gpt
    instruction: you are a helpful assistant
```

Examples (Anthropic):

```yaml
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
    thinking_budget: 1024

agents:
  root:
    model: claude
    instruction: you are a helpful assistant that doesn't think very much
```

Examples (Google):

```yaml
models:
  gemini-no-thinking:
    provider: google
    model: gemini-2.5-flash
    thinking_budget: 0 # Disable thinking

  gemini-dynamic:
    provider: google
    model: gemini-2.5-flash
    thinking_budget: -1 # Dynamic thinking (model decides)

  gemini-fixed:
    provider: google
    model: gemini-2.5-flash
    thinking_budget: 8192 # Fixed token budget

agents:
  root:
    model: gemini-fixed
    instruction: you are a helpful assistant
```

#### Interleaved Thinking (Anthropic)

Anthropic's interleaved thinking feature uses the Beta Messages API to provide
tool calling during model reasoning. You can control this behavior using the
`interleaved_thinking` provider option:

```yaml
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
    thinking_budget: 8192 # Optional: defaults to 16384 when interleaved thinking is enabled
    provider_opts:
      interleaved_thinking: true # Enable interleaved thinking (default: false)
```

Notes:

- **OpenAI**: If an invalid effort value is set, the request will fail with a
  clear error
- **Anthropic**: Values < 1024 or ≥ `max_tokens` are ignored (warning logged).
  When `interleaved_thinking` is enabled, Docker `cagent` uses Anthropic's Beta
  Messages API with a default thinking budget of 16384 tokens if not specified
- **Google**:
  - Most models support values between -1 and 24576 tokens. Set to `0` to
    disable, `-1` for dynamic thinking
  - Gemini 2.5 Pro: supports 128–32768 tokens. Cannot be disabled (minimum 128)
  - Gemini 2.5 Flash-Lite: supports 512–24576 tokens. Set to `0` to disable,
    `-1` for dynamic thinking
- For unsupported providers, `thinking_budget` has no effect
- Debug logs include the applied effort (e.g., "OpenAI request using
  thinking_budget", "Gemini request using thinking_budget")

See `examples/thinking_budget.yaml` for a complete runnable demo.

#### Model Examples

> ⚠️ **NOTE** ⚠️  
> **More model names can be found [here](https://modelname.ai/)**

```yaml

# OpenAI
models:
  gpt:
    provider: openai
    model: gpt-5-mini

# Anthropic
models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0

# Gemini
models:
  gemini:
    provider: google
    model: gemini-2.5-flash

# AWS Bedrock
models:
  claude-bedrock:
    provider: amazon-bedrock
    model: global.anthropic.claude-sonnet-4-5-20250929-v1:0  # Global inference profile

# Docker Model Runner (DMR)
models:
  qwen:
    provider: dmr
    model: ai/qwen3
```

#### AWS Bedrock provider usage

**Prerequisites:**

- AWS account with Bedrock enabled in your region
- Model access granted in the [Bedrock
  Console](https://console.aws.amazon.com/bedrock/) (some models require
  approval)
- AWS credentials configured (see authentication below)

**Authentication:**

Bedrock supports two authentication methods:

**Option 1: Bedrock API key** (simplest)

Set the `AWS_BEARER_TOKEN_BEDROCK` environment variable with your Bedrock API
key. You can customize the env var name using `token_key`:

```yaml
models:
  claude-bedrock:
    provider: amazon-bedrock
    model: global.anthropic.claude-sonnet-4-5-20250929-v1:0
    token_key: AWS_BEARER_TOKEN_BEDROCK # Name of env var containing your token (default)
    provider_opts:
      region: us-east-1
```

Generate API keys in the [Bedrock
Console](https://console.aws.amazon.com/bedrock/) under "API keys".

**Option 2: AWS credentials** (default)

Uses the [AWS SDK default credential
chain](https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials):

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. Shared credentials file (`~/.aws/credentials`)
3. Shared config file (`~/.aws/config` with `AWS_PROFILE`)
4. IAM instance roles (EC2, ECS, Lambda)

You can also use `provider_opts.role_arn` for cross-account role assumption.

**Basic usage with AWS profile:**

```yaml
models:
  claude-bedrock:
    provider: amazon-bedrock
    model: global.anthropic.claude-sonnet-4-5-20250929-v1:0
    max_tokens: 64000
    provider_opts:
      profile: my-aws-profile
      region: us-east-1
```

**With IAM role assumption:**

```yaml
models:
  claude-bedrock:
    provider: amazon-bedrock
    model: anthropic.claude-3-sonnet-20240229-v1:0
    provider_opts:
      role_arn: "arn:aws:iam::123456789012:role/BedrockAccessRole"
      external_id: "my-external-id"
```

**provider_opts for Bedrock:**

| Option              | Type   | Description                     | Default                |
| ------------------- | ------ | ------------------------------- | ---------------------- |
| `region`            | string | AWS region                      | us-east-1              |
| `profile`           | string | AWS profile name                | (default chain)        |
| `role_arn`          | string | IAM role ARN for assume role    | (none)                 |
| `role_session_name` | string | Session name for assumed role   | cagent-bedrock-session |
| `external_id`       | string | External ID for role assumption | (none)                 |
| `endpoint_url`      | string | Custom endpoint (VPC/testing)   | (none)                 |

**Supported models (via Converse API):**

All Bedrock models that support the Converse API work with cagent. Use inference
profile IDs for best availability:

- **Anthropic Claude**: `global.anthropic.claude-sonnet-4-5-20250929-v1:0`,
  `us.anthropic.claude-haiku-4-5-20251001-v1:0`
- **Amazon Nova**: `global.amazon.nova-2-lite-v1:0`
- **Meta Llama**: `us.meta.llama3-2-90b-instruct-v1:0`
- **Mistral**: `us.mistral.mistral-large-2407-v1:0`

**Inference profile prefixes:**

| Prefix    | Routes to                                |
| --------- | ---------------------------------------- |
| `global.` | All commercial AWS regions (recommended) |
| `us.`     | US regions only                          |
| `eu.`     | EU regions only (GDPR compliance)        |

```yaml
models:
  claude-global:
    provider: amazon-bedrock
    model: global.anthropic.claude-sonnet-4-5-20250929-v1:0 # Routes to any available region
```

#### DMR (Docker Model Runner) provider usage

If `base_url` is omitted, Docker `cagent` will use
`http://localhost:12434/engines/llama.cpp/v1` by default

You can pass DMR runtime (e.g. llama.cpp) options using

```
models:
  provider: dmr
  provider_opts:
    runtime_flags:
```

The context length is taken from `max_tokens` at the model level:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    # base_url: omitted -> auto-discovery via Docker Model plugin
    provider_opts:
      runtime_flags: ["--ngl=33", "--top-p=0.9"]
```

`runtime_flags` also accepts a single string with comma or space separation:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    provider_opts:
      runtime_flags: "--ngl=33 --top-p=0.9"
```

Explicit `base_url` example with multiline runtime_flags string:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    base_url: http://127.0.0.1:12434/engines/llama.cpp/v1
    provider_opts:
      runtime_flags: |
        --ngl=33
        --top-p=0.9
```

Requirements and notes:

- Docker Model plugin must be available for auto-configure/auto-discovery
  - Verify with: `docker model status --json`
- Configuration is best-effort; failures fall back to the default base URL
- `provider_opts` currently apply to `dmr`, `anthropic`, and `amazon-bedrock`
  providers
- `runtime_flags` are passed after `--` to the inference runtime (e.g.,
  llama.cpp)

Parameter mapping and precedence (DMR):

- `ModelConfig` fields are translated into engine-specific runtime flags. For
  e.g. with the `llama.cpp` backend:
  - `temperature` → `--temp`
  - `top_p` → `--top-p`
  - `frequency_penalty` → `--frequency-penalty`
  - `presence_penalty` → `--presence-penalty` ...
- `provider_opts.runtime_flags` always take priority over derived flags on
  conflict. When a conflict is detected, Docker `cagent` logs a warning
  indicating the overridden flag. `max_tokens` is the only exception for now

Examples:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    temperature: 0.5 # derives --temp 0.5
    top_p: 0.9 # derives --top-p 0.9
    max_tokens: 8192 # sets --context-size=8192
    provider_opts:
      runtime_flags: ["--temp", "0.7", "--threads", "8"] # overrides derived --temp, sets --threads
```

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    provider_opts:
      runtime_flags: "--ngl=33 --repeat-penalty=1.2" # string accepted as well
```

##### Speculative Decoding

DMR supports speculative decoding for faster inference by using a smaller draft
model to predict tokens ahead. Configure speculative decoding using
`provider_opts`:

```yaml
models:
  qwen-with-speculative:
    provider: dmr
    model: ai/qwen3:14B
    max_tokens: 8192
    provider_opts:
      speculative_draft_model: ai/qwen3:0.6B-F16 # Draft model for predictions
      speculative_num_tokens: 16 # Number of tokens to generate speculatively
      speculative_acceptance_rate: 0.8 # Acceptance rate threshold
```

All three speculative decoding options are passed to `docker model configure` as
flags:

- `speculative_draft_model` → `--speculative-draft-model`
- `speculative_num_tokens` → `--speculative-num-tokens`
- `speculative_acceptance_rate` → `--speculative-acceptance-rate`

These options work alongside `max_tokens` (which sets `--context-size`) and
`runtime_flags`.

##### Troubleshooting:

- Plugin not found: Docker `cagent` will log a debug message and use the default
  base URL
- Endpoint empty in status: ensure the Model Runner is running, or set
  `base_url` manually
- Flag parsing: if using a single string, quote properly in YAML; you can also
  use a list

### Alloy models

"Alloy models" essentially means using more than one model in the same chat
context. Not at the same time, but "randomly" throughout the conversation to try
to take advantage of the strong points of each model.

More information on the idea can be found
[here](https://xbow.com/blog/alloy-agents)

To have an agent use an alloy model, you can define more than one model in the
`model` field, separated by commas.

Example:

```yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-0,openai/gpt-5-mini
    ...
```

### Tool Configuration

### Available MCP Tools

Common MCP tools include:

- **Filesystem**: Read/write files
- **Shell**: Execute shell commands
- **Database**: Query databases
- **Web**: Make HTTP requests
- **Git**: Version control operations
- **Browser**: Web browsing and automation
- **Code**: Programming language specific tools
- **API**: REST API integration tools

### Configuring MCP Tools

**Local (stdio) MCP Server:**

```yaml
toolsets:
  - type: mcp # Model Context Protocol
    command: string # Command to execute
    args: [] # Command arguments
    tools: [] # Optional: List of specific tools to enable
    env: [] # Environment variables for this tool
    env_file: [] # Environment variable files
```

Example:

```yaml
toolsets:
  - type: mcp
    command: rust-mcp-filesystem
    args: ["--allow-write", "."]
    tools: ["read_file", "write_file"] # Optional: specific tools only
    env:
      - "RUST_LOG=debug"
```

**Remote (SSE) MCP Server:**

```yaml
toolsets:
  - type: mcp # Model Context Protocol
    remote:
      url: string # Base URL to connect to
      transport_type: string # Type of MCP transport (sse or streamable)
      headers:
        key: value # HTTP headers. Mainly used for auth
    tools: [] # Optional: List of specific tools to enable
```

Example:

```yaml
toolsets:
  - type: mcp
    remote:
      url: "https://mcp-server.example.com"
      transport_type: "sse"
      headers:
        Authorization: "Bearer your-token-here"
    tools: ["search_web", "fetch_url"]
```

### Using tools via the Docker MCP Gateway

We recommend running containerized MCP tools, for security and resource
isolation. Under the hood, `cagent` will run them with the [Docker MCP
Gateway](https://github.com/docker/mcp-gateway) so that all the tools in the
`Docker MCP Catalog` can be accessed through a single endpoint.

In this example, lets configure `duckduckgo` to give our agents the ability to
search the web:

```yaml
toolsets:
  - type: mcp
    ref: docker:duckduckgo
```

### Installing MCP Tools

Example installation of local tools with `npm`:

```bash
# Install Rust-based MCP filesystem tool
npm i -g @rustmcp/rust-mcp-filesystem@latest

# Install other popular MCP tools
npm install -g @modelcontextprotocol/server-filesystem
npm install -g @modelcontextprotocol/server-git
npm install -g @modelcontextprotocol/server-web
```

## Built-in Tools

Included in `cagent` are a series of built-in tools that can greatly enhance the
capabilities of your agents without needing to configure any external MCP tools.

**Configuration example**

```yaml
toolsets:
  - type: filesystem # Grants the agent filesystem access
  - type: think # Enables the think tool
  - type: todo # Enable the todo list tool
    shared: boolean # Should the todo list be shared between agents (optional)
  - type: memory # Allows the agent to store memories to a local sqlite db
    path: ./mem.db # Path to the sqlite database for memory storage (optional)
```

Let's go into a bit more detail about the built-in tools that agents can use:

### Think Tool

The think tool allows agents to reason through problems step by step:

```yaml
agents:
  root:
    # ... other config
    toolsets:
      - type: think
```

### Todo Tool

The todo tool helps agents manage task lists:

```yaml
agents:
  root:
    # ... other config
    toolsets:
      - type: todo
```

### Memory Tool

The memory tool provides persistent storage:

```yaml
agents:
  root:
    # ... other config
    toolsets:
      - type: memory
        path: "./agent_memory.db"
```

### Task Transfer Tool

All agents automatically have access to the task transfer tool, which allows
them to delegate tasks to other agents:

```
transfer_task(agent="developer", task="Create a login form", expected_output="HTML and CSS code")
```

## Advanced Features

### Agent Store and Distribution

cagent supports distributing via, and running agents from, Docker registries:

```bash
# Pull an agent from a registry
./bin/cagent pull docker.io/username/my-agent:latest

# Push your agent to a registry
./bin/cagent push docker.io/username/my-agent:latest

# Run an agent directly from an image reference
./bin/cagent run docker.io/username/my-agent:latest
```

**Agent References:**

- File agents: `my-agent.yaml` (relative path)
- Store agents: `docker.io/username/my-agent:latest` (full Docker reference)

## Troubleshooting

### Common Issues

**Agent not responding:**

- Check API keys are set correctly
- Verify model name matches provider
- Check network connectivity

**Tool errors:**

- Ensure MCP tools are installed and accessible
- Check file permissions for filesystem tools
- Verify tool arguments and command paths
- Test MCP tools independently before integration
- Check tool lifecycle (start/stop) in debug logs

**Configuration errors:**

- Validate YAML syntax
- Check all referenced agents exist
- Ensure all models are defined
- Verify toolset configurations
- Check agent hierarchy (sub_agents references)

**Session and connectivity issues:**

- Verify port availability for MCP server modes
- Test MCP endpoint accessibility (curl test)
- Verify client isolation in multi-tenant scenarios
- Check session timeouts and limits

**Performance issues:**

- Monitor memory usage with multiple concurrent sessions
- Check for tool resource leaks
- Verify proper session cleanup
- Monitor streaming response performance

### Debug Mode

Enable debug logging for detailed information:

```bash
# CLI mode
./bin/cagent run config.yaml --debug
```

### Log Analysis

Check logs for:

- API call errors and rate limiting
- Tool execution failures and timeouts
- Configuration validation issues
- Network connectivity problems
- MCP protocol handshake issues
- Session creation and cleanup events
- Client isolation boundary violations

### Agent Store Issues

```bash
# Test Docker registry connectivity
docker pull docker.io/username/agent:latest

# Verify agent content
./bin/cagent pull docker.io/username/agent:latest
```

## Integration Examples

### Custom Memory Strategies

Implement persistent memory across sessions:

```yaml
agents:
  researcher:
    model: claude
    instruction: |
      You are a research assistant with persistent memory.
      Remember important findings and reference previous research.
    toolsets:
      - type: memory
        path: ./research_memory.db
```

### Multi-Model Teams

```yaml
models:
  # Local model for fast responses
  claude_local:
    provider: anthropic
    model: claude-sonnet-4-0
    temperature: 0.2

  gpt4:
    provider: openai
    model: gpt-4o
    temperature: 0.1

  # Creative model for content generation
  gpt4_creative:
    provider: openai
    model: gpt-4o
    temperature: 0.8

agents:
  analyst:
    model: claude_local
    description: Fast analysis and reasoning

  coder:
    model: gpt4
    description: not very creative developer

  writer:
    model: gpt4_creative
    description: Creative content generation
```

This guide should help you get started with Docker `cagent` and build powerful
multi-agent systems.
