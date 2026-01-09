### Improving an agent with MCP tools

Docker `cagent` supports MCP servers, enabling agents to use a wide variety of
external tools and services.

It supports all transport types: `stdio`, `http` and `sse`.

Giving an agent access to tools via MCP is a good way to greatly improve its
capabilities, the quality of its results and its general usefulness.

Get started with the [Docker MCP
Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/) and
[catalog](https://docs.docker.com/ai/mcp-catalog-and-toolkit/catalog/)

Here, we're giving the same basic agent from the example above access to a
**containerized** `duckduckgo` mcp server and its tools by using Docker's MCP
Gateway:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo
```

When using a containerized server via the `Docker MCP gateway`, you can
configure any required settings/secrets/authentication using the [Docker MCP
Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/#example-use-the-github-official-mcp-server)
in Docker Desktop.

Aside from the containerized MCP servers the `Docker MCP Gateway` provides, any
standard MCP server can be used with Docker `cagent`!

Here's an example similar to the above but adding `read_file` and `write_file`
tools from the `rust-mcp-filesystem` MCP server:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses. Write your search results to disk.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo
      - type: mcp
        command: rust-mcp-filesystem # installed with `cargo install rust-mcp-filesystem`
        args: ["--allow-write", "."]
        tools: ["read_file", "write_file"] # Optional: specific tools only
        env:
          - "RUST_LOG=debug"
```

See [the USAGE docs](./docs/usage.md#tool-configuration) for more detailed
information and examples
