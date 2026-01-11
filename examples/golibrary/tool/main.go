package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/run"
	"github.com/docker/cagent/pkg/tools"
)

type AddNumbersArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

func addNumbers(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var p AddNumbersArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &p); err != nil {
		return nil, err
	}

	fmt.Println("Adding numbers", p.A, p.B)

	return tools.ResultSuccess(fmt.Sprintf("%d", p.A+p.B)), nil
}

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

	toolAddNumbers := tools.Tool{
		Name:        "add",
		Category:    "compute",
		Description: "Add two numbers",
		Parameters:  tools.MustSchemaFor[AddNumbersArgs](),
		Handler:     addNumbers,
	}

	calculator := agent.New(
		"root",
		"You are a calculator.",
		agent.WithModel(llm),
		agent.WithTools(toolAddNumbers),
	)

	response, err := run.Agent(ctx, calculator, "What is 1 + 2?")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(response)
}
