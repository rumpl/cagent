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
)

func main() {
	ctx := context.Background()

	llm, err := openai.NewClient(
		ctx,
		&latest.ModelConfig{
			Model: "gpt-4o",
		},
		environment.NewDefaultProvider(),
	)
	if err != nil {
		log.Fatal(err)
	}

	human := agent.New(
		"root",
		"You are a human, with feelings and emotions.",
		agent.WithModel(llm),
	)

	response, err := run.Agent(ctx, human, "How are you doing?")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(response)
}
