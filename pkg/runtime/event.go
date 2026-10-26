package runtime

import (
	"time"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/runtime/contract"
)

func newAgentContext(name string) contract.AgentContext {
	return contract.AgentContext{AgentName: name, Timestamp: time.Now()}
}

func userMessageEvent(msg chat.Message, sessionID string, position int) contract.Event {
	event := contract.UserMessage(msg.Content, sessionID, msg.MultiContent, position).(*contract.UserMessageEvent)
	event.MessageID = msg.MessageID
	return event
}

func BudgetUsage(sessionID, agentName string, snaps []namedBudgetSnapshot) contract.Event {
	budgets := make([]contract.BudgetStatus, 0, len(snaps))
	for _, ns := range snaps {
		s := ns.Snapshot
		var perAgent []contract.AgentBudgetUsage
		if len(s.PerAgent) > 0 {
			perAgent = make([]contract.AgentBudgetUsage, len(s.PerAgent))
			for i, a := range s.PerAgent {
				perAgent[i] = contract.AgentBudgetUsage{
					AgentName:     a.AgentName,
					Cost:          a.Cost,
					Tokens:        a.Tokens,
					ActiveSeconds: a.Active.Seconds(),
				}
			}
		}
		budgets = append(budgets, contract.BudgetStatus{
			Name:           ns.Name,
			Cost:           s.Cost,
			MaxCost:        s.MaxCost,
			Tokens:         s.Tokens,
			MaxTokens:      s.MaxTokens,
			ElapsedSeconds: s.Elapsed.Seconds(),
			MaxTimeSeconds: s.MaxTime.Seconds(),
			Unpriced:       s.Unpriced,
			PerAgent:       perAgent,
		})
	}
	return &contract.BudgetUsageEvent{
		Type:         "budget_usage",
		SessionID:    sessionID,
		AgentContext: newAgentContext(agentName),
		Budgets:      budgets,
	}
}

func BudgetExceeded(sessionID, agentName string, br budgetBreach, stopMessage *chat.Message) contract.Event {
	var stop *contract.BudgetStopMessage
	if stopMessage != nil {
		stop = &contract.BudgetStopMessage{
			AgentName: agentName,
			Message: contract.BudgetStopChatMessage{
				Role:      stopMessage.Role,
				Content:   stopMessage.Content,
				CreatedAt: stopMessage.CreatedAt,
			},
		}
	}
	return &contract.BudgetExceededEvent{
		Type:         "budget_exceeded",
		SessionID:    sessionID,
		AgentContext: newAgentContext(agentName),
		Budget:       br.Budget,
		Limit:        string(br.Limit),
		Used:         br.Used,
		Max:          br.Max,
		ConfigPath:   br.configPath(),
		Message:      br.Message(),
		StopMessage:  stop,
	}
}
