package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/auth"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools/builtin"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		log.Println(err)
	}
}

func run(ctx context.Context) error {
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		TokenKey: "OPENAI_API_KEY",
	}
	env := environment.NewDefaultProvider()
	llm, err := openai.NewClient(ctx, cfg, env, auth.ProviderFor(ctx, cfg, env, options.ModelOptions{}))
	if err != nil {
		return err
	}

	agents := team.New(
		team.WithAgents(
			agent.New(
				"root",
				"You are an expert hacker",
				agent.WithModel(llm),
				agent.WithToolSets(builtin.NewShellTool(os.Environ(), &config.RuntimeConfig{Config: config.Config{WorkingDir: "/tmp"}}, nil)),
			),
		),
	)
	rt, err := runtime.New(agents)
	if err != nil {
		return err
	}

	sess := session.New(session.WithUserMessage("Tell me a story about my current directory"))

	messages, err := rt.Run(ctx, sess)
	if err != nil {
		return err
	}

	fmt.Println(messages[len(messages)-1].Message.Content)
	return nil
}
