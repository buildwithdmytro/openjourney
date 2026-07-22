// Package agent implements a bounded, governed ReAct agentic assistant.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/ai/tools"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

var (
	ErrMaxStepsExceeded = errors.New("agent max steps exceeded")
	stepOutputSchema    = json.RawMessage(`{
		"type": "object",
		"required": ["action"],
		"properties": {
			"action": {
				"type": "string",
				"enum": ["tool", "final"]
			},
			"tool": {
				"type": "string"
			},
			"args": {
				"type": "object"
			},
			"answer": {
				"type": "string"
			}
		},
		"additionalProperties": false
	}`)
)

type Config struct {
	MaxSteps int
	Timeout  time.Duration
}

type StepTrace struct {
	StepIndex  int             `json:"step_index"`
	Action     string          `json:"action"` // "tool" or "final"
	Tool       string          `json:"tool,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	ActivityID string          `json:"activity_id,omitempty"`
}

type RunResult struct {
	Question        string      `json:"question"`
	Answer          string      `json:"answer"`
	Trace           []StepTrace `json:"trace"`
	Status          string      `json:"status"` // "completed", "max_steps_exceeded", "budget_exceeded", "timeout", "error"
	FinalActivityID string      `json:"final_activity_id,omitempty"`
}

type Agent struct {
	gateway  *ai.Gateway
	runner   *tools.Runner
	tools    []tools.Tool
	maxSteps int
	timeout  time.Duration
}

func NewAgent(gateway *ai.Gateway, runner *tools.Runner, toolList []tools.Tool, cfg Config) *Agent {
	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 6
	}
	return &Agent{
		gateway:  gateway,
		runner:   runner,
		tools:    toolList,
		maxSteps: maxSteps,
		timeout:  cfg.Timeout,
	}
}

func (a *Agent) Run(ctx context.Context, caller domain.Principal, question string) (*RunResult, error) {
	if a.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.timeout)
		defer cancel()
	}

	result := &RunResult{
		Question: question,
		Status:   "completed",
		Trace:    make([]StepTrace, 0),
	}

	var toolDescs []string
	for _, t := range a.tools {
		def := t.Definition()
		toolDescs = append(toolDescs, fmt.Sprintf("- Tool: %q, Purpose: %q, RequiredScopes: %v", def.Name, def.Purpose, def.RequiredScopes))
	}
	toolsContext := strings.Join(toolDescs, "\n")

	for step := 0; step < a.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			result.Status = "timeout"
			result.Answer = fmt.Sprintf("Execution timed out after %d steps.", len(result.Trace))
			return result, ai.ErrCallTimeout
		}

		prompt := a.buildStepPrompt(question, toolsContext, result.Trace)

		genReq := ai.GenerateRequest{
			Action:       "ai.agent.step",
			Prompt:       prompt,
			OutputSchema: stepOutputSchema,
			Timeout:      a.timeout,
		}

		resp, err := a.gateway.Generate(ctx, caller, genReq)
		if err != nil {
			if errors.Is(err, ai.ErrCallBudgetExceeded) || errors.Is(err, ai.ErrBudgetExceeded) {
				result.Status = "budget_exceeded"
				if result.Answer == "" {
					result.Answer = "Agent budget exceeded."
				}
				return result, err
			}
			if errors.Is(err, ai.ErrCallTimeout) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				result.Status = "timeout"
				if result.Answer == "" {
					result.Answer = "Agent call timed out."
				}
				return result, ai.ErrCallTimeout
			}
			result.Status = "error"
			return result, err
		}

		var choice struct {
			Action string          `json:"action"`
			Tool   string          `json:"tool"`
			Args   json.RawMessage `json:"args"`
			Answer string          `json:"answer"`
		}
		if unmarshalErr := json.Unmarshal([]byte(resp.Content), &choice); unmarshalErr != nil {
			result.Status = "error"
			return result, fmt.Errorf("failed to unmarshal agent step choice: %w", unmarshalErr)
		}

		if choice.Action == "final" {
			stepTrace := StepTrace{
				StepIndex:  step,
				Action:     "final",
				ActivityID: resp.ActivityID,
			}
			result.Trace = append(result.Trace, stepTrace)
			result.Answer = choice.Answer
			result.FinalActivityID = resp.ActivityID
			result.Status = "completed"
			return result, nil
		}

		if choice.Action == "tool" {
			stepTrace := StepTrace{
				StepIndex:  step,
				Action:     "tool",
				Tool:       choice.Tool,
				Args:       choice.Args,
				ActivityID: resp.ActivityID,
			}

			if a.runner == nil {
				stepTrace.Error = "tool runner unavailable"
				result.Trace = append(result.Trace, stepTrace)
				continue
			}

			toolOutput, callErr := a.runner.Call(ctx, caller, choice.Tool, choice.Args)
			if callErr != nil {
				stepTrace.Error = callErr.Error()
			} else {
				stepTrace.Result = toolOutput
			}
			result.Trace = append(result.Trace, stepTrace)
		}
	}

	result.Status = "max_steps_exceeded"
	if result.Answer == "" {
		result.Answer = fmt.Sprintf("Reached maximum step limit (%d) without completing final answer.", a.maxSteps)
	}
	return result, ErrMaxStepsExceeded
}

func (a *Agent) buildStepPrompt(question string, toolsContext string, trace []StepTrace) string {
	var sb strings.Builder
	sb.WriteString("You are a governed AI agentic assistant. Answer the user's question using the provided read-only tools.\n\n")
	sb.WriteString("User Question: ")
	sb.WriteString(question)
	sb.WriteString("\n\nAvailable Tools:\n")
	sb.WriteString(toolsContext)
	sb.WriteString("\n\nInstructions:\n")
	sb.WriteString("- Respond strictly with a JSON object conforming to the output schema.\n")
	sb.WriteString("- Set action to 'tool' to execute a tool, specifying tool name and args object.\n")
	sb.WriteString("- Set action to 'final' when ready to deliver the final grounded answer.\n")

	if len(trace) > 0 {
		sb.WriteString("\nExecution History:\n")
		for _, st := range trace {
			if st.Action == "tool" {
				sb.WriteString(fmt.Sprintf("Step %d: Called tool %q with args: %s\n", st.StepIndex, st.Tool, string(st.Args)))
				if st.Error != "" {
					sb.WriteString(fmt.Sprintf("  -> Tool Error: %s\n", st.Error))
				} else {
					sb.WriteString(fmt.Sprintf("  -> Tool Result: %s\n", string(st.Result)))
				}
			}
		}
	}

	return sb.String()
}
