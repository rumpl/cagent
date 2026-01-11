package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/run"
	"github.com/docker/cagent/pkg/tools/builtin"
)

func main() {
	ctx := context.Background()
	llm, err := openai.NewClient(
		ctx,
		&latest.ModelConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
		environment.NewDefaultProvider(),
	)
	if err != nil {
		log.Fatal(err)
	}

	hacker := agent.New(
		"root",
		"You are an expert hacker",
		agent.WithModel(llm),
		agent.WithToolSets(builtin.NewShellTool(os.Environ(), &config.RuntimeConfig{Config: config.Config{WorkingDir: "/tmp"}}, nil)),
	)

	response, err := run.Agent(ctx, hacker, "Tell me a story about my current directory")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(response)
}
