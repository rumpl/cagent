# Running Docker `cagent` in MCP Mode

## Why use `cagent mcp`?

The `cagent mcp` command allows your agents to be consumed by other MCP-compatible products and tools. This enables seamless integration with existing workflows and applications that support the Model Context Protocol (MCP).

**Important:** MCP is not just about tools - it's also about agents. By exposing your Docker `cagent` configurations through MCP, you make your specialized agents available to any MCP client, whether that's Claude Desktop, Claude Code, or any other MCP-compatible application.

This means you can:
- Use your custom agents directly within Claude Desktop or Claude Code
- Share agents across different applications
- Build reusable agent teams that can be consumed anywhere MCP is supported
- Integrate domain-specific agents into your existing development workflows

## Using Docker `cagent` agents in Claude Desktop

To use your Docker `cagent` agents in Claude Desktop, add a configuration to your Claude Desktop MCP settings file:

**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Here's an example configuration:

```json
{
  "mcpServers": {
    "myagent": {
      "command": "/Users/dockereng/bin/cagent",
      "args": ["mcp", "dockereng/myagent", "--working-dir", "/Users/dockereng/src"],
      "env": {
        "PATH": "/Applications/Docker.app/Contents/Resources/bin:${PATH}",
        "ANTHROPIC_API_KEY": "your_anthropic_key_here",
        "OPENAI_API_KEY": "your_openai_key_here"
      }
    }
  }
}
```

### Configuration breakdown:

- **command**: Full path to your `cagent` binary
- **args**: The MCP command arguments:
  - `mcp`: The subcommand to run Docker `cagent` in MCP mode
  - `dockereng/myagent`: Your agent configuration (can be a local file path or OCI reference)
  - `--working-dir`: Optional working directory for the agent
- **env**: Environment variables needed by your agents:
  - `PATH`: Include any additional paths needed (e.g., Docker binaries)
  - `ANTHROPIC_API_KEY`: Required if your agents use Anthropic models
  - `OPENAI_API_KEY`: Required if your agents use OpenAI models
  - Add any other API keys your agents need (GOOGLE_API_KEY, XAI_API_KEY, etc.)

After updating the configuration, restart Claude Desktop. Your agents will now appear as available tools in Claude Desktop's interface.

## Using Docker `cagent` agents in Claude Code

To add your Docker `cagent` agents to Claude Code, use the `claude mcp add` command:

```bash
claude mcp add --transport stdio myagent \
  --env OPENAI_API_KEY=$OPENAI_API_KEY \
  --env ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  -- cagent mcp agentcatalog/pirate --working-dir $(pwd)
```

### Command breakdown:

- `claude mcp add`: Claude Code command to add an MCP server
- `--transport stdio`: Use stdio transport (standard for local MCP servers)
- `myagent`: Name for this MCP server in Claude Code
- `--env`: Pass through required environment variables (repeat for each variable)
- `--`: Separates Claude Code arguments from the MCP server command
- `cagent mcp agentcatalog/pirate`: The Docker `cagent` MCP command with your agent reference
- `--working-dir $(pwd)`: Set the working directory for the agent

After adding the MCP server, your agents will be available as tools within Claude Code sessions.

## Agent references

You can specify your agent configuration in several ways:

```bash
# Local file path
cagent mcp ./examples/dev-team.yaml

# OCI artifact from Docker Hub
cagent mcp agentcatalog/pirate

# OCI artifact with namespace
cagent mcp dockereng/myagent
```

## Additional options

The `cagent mcp` command supports additional options:

- `--working-dir <path>`: Set the working directory for agent execution
- `--log-level <level>`: Set logging level (debug, info, warn, error)

## Example: Multi-agent team in MCP

When you expose a multi-agent team configuration via MCP, each agent becomes a separate tool. For example, with this configuration:

```yaml
agents:
  root:
    model: claude-sonnet-4-0
    description: "Main coordinator agent"
    instruction: "You coordinate tasks and delegate to specialists"
    sub_agents: ["designer", "engineer"]

  designer:
    model: gpt-5-mini
    description: "UI/UX design specialist"
    instruction: "You create user interface designs and mockups"

  engineer:
    model: claude-sonnet-4-0
    description: "Software engineering specialist"
    instruction: "You implement code based on requirements"
```

All three agents (`root`, `designer`, and `engineer`) will be available as separate tools in the MCP client, allowing you to interact with specific specialists directly or use the root coordinator.

## Troubleshooting

### Agents not appearing in Claude Desktop/Code

1. Verify your `cagent` binary path is correct
2. Check that all required API keys are set in the environment variables
3. Restart the MCP client (Claude Desktop/Code) after configuration changes
4. Check the MCP client logs for connection errors

### Permission errors

Make sure your `cagent` binary has execute permissions:

```bash
chmod +x /path/to/cagent
```

### Working directory issues

If your agents need access to specific files or directories, ensure the `--working-dir` parameter points to the correct location and that the agent has appropriate permissions.
