# Docker `cagent`

Docker `cagent` lets you create and run intelligent AI agents, where each agent
has specialized knowledge, tools and capabilities.

Think of it as allowing you to quickly build, share and run a team of virtual
experts that collaborate to solve complex problems for you.

**And it's dead easy to use! In most cases, you don't need to write a single
line of code.**

![cagent in action](docs/demo.gif)

# Features

- **Multi-agent architecture** - Create specialized agents for different
  domains.
- **Rich tool ecosystem** - Agents can use external tools and APIs via the MCP
  protocol.
- **Smart delegation** - Agents can automatically route tasks to the most
  suitable specialist.
- **YAML configuration** - Declarative model and agent configuration.
- **RAG (Retrieval-Augmented Generation)** - Pluggable retrieval strategies
  (BM25, chunked-embeddings, semantic-embeddings) with hybrid retrieval, result
  fusion and reranking support.
- **AI provider agnostic** - Support for OpenAI, Anthropic, Gemini, AWS Bedrock,
  xAI, Mistral, Nebius and [Docker Model
  Runner](https://docs.docker.com/ai/model-runner/).

## Installation

### It ships with Docker Desktop!

Starting with [Docker Desktop
4.49.0](https://docs.docker.com/desktop/release-notes/#4490), Docker `cagent` is
automatically installed.

```sh
$ cagent run default "Hi, what can you do for me?"
```

### Using Homebrew

As an alternative, it's also on [homebrew](https://brew.sh/):

```sh
$ brew install cagent
```

### Using binary releases

Finally, [Prebuilt binaries](https://github.com/docker/cagent/releases) for
Windows, macOS and Linux can be found on the release page of the [project's
GitHub repository](https://github.com/docker/cagent/releases).

Once you've downloaded the appropriate binary for your platform, you may need to
give it executable permissions. On macOS and Linux, this is done with the
following command:

```sh
# linux amd64 build example
chmod +x /path/to/downloads/cagent-linux-amd64
```

You can then rename the binary to Docker `cagent` and configure your `PATH` to
be able to find it (configuration varies by platform).

### **Set your API keys**

Based on the models you configure your agents to use, you will need to set the
corresponding provider API key accordingly, all these keys are optional, you
will likely need at least one of these, though:

```bash
export OPENAI_API_KEY=your_api_key_here      # For OpenAI models
export ANTHROPIC_API_KEY=your_api_key_here   # For Anthropic models
export GOOGLE_API_KEY=your_api_key_here      # For Gemini models
export XAI_API_KEY=your_api_key_here         # For xAI models
export NEBIUS_API_KEY=your_api_key_here      # For Nebius models
export MISTRAL_API_KEY=your_api_key_here     # For Mistral models
```

## Your First Agent

Creating agents with Docker `cagent` is straightforward. They are described in a
`.yaml` file, like this one:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
```

- Follow the installation instructions.
- Create a`basic_agent.yaml` file with the above content.
- Run it in a terminal with `cagent run basic_agent.yaml`.

Many more examples can be found [in the examples directory](/examples)!

## Usage

More details on the usage and configuration of Docker `cagent` can be found in
[usage.md](/docs/usage.md)

## Telemetry

We track anonymous usage data to improve the tool. See
[telemetry.md](/docs/telemetry.md) for details.

## Contributing

Want to hack on Docker `cagent`, or help us fix bugs and build out some
features? 🔧

Read the information on how to build from source and contribute to the project
in [CONTRIBUTING.md](/docs/CONTRIBUTING.md)

Meta: use Docker `cagent` to code on Docker `cagent`

A smart way to improve Docker `cagent`'s codebase and feature set is to do it
with the help of a Docker `cagent` agent!

We have one that we use and that you should use too:

```sh
cd cagent
cagent run ./golang_developer.yaml
```

This agent is an _expert Golang developer specializing in the Docker `cagent`
multi-agent AI system architecture_.

Ask it anything about Docker `cagent`. It can be questions about the current
code or about improvements to the code. It can also fix issues and implement new
features!

## Share your feedback

We’d love to hear your thoughts on this project. You can find us on
[Slack](https://dockercommunity.slack.com/archives/C09DASHHRU4)
