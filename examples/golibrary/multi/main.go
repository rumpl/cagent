package main

import (
	"context"
	"fmt"
	"log"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/run"
	"github.com/docker/cagent/pkg/team"
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

	child := agent.New(
		"child",
		"You are a child, with a lot of energy.",
		agent.WithModel(llm),
		agent.WithDescription("A child."),
	)

	root := agent.New(
		"root",
		"You are a human, with feelings and emotions.",
		agent.WithModel(llm),
		agent.WithSubAgents(child),
		agent.WithToolSets(builtin.NewTransferTaskTool()),
	)

	t := team.New(team.WithAgents(root, child))

	response, err := run.Team(ctx, t, "Ask your child how they are doing and tell me what they said")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(response)
}
