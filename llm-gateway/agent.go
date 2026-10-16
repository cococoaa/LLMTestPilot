package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"
)

func (g *Gateway) runAgent(ctx context.Context, task *Task) {
	task.Status = "running"
	task.Steps = nil
	g.tasks.Save(task)
	g.events.Broadcast(task.ID, Event{Type: "status", Data: map[string]string{"status": "running"}})

	browser, err := NewBrowserSession()
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("failed to start browser: %v", err)
		g.tasks.Save(task)
		g.events.Broadcast(task.ID, Event{Type: "error", Data: map[string]string{"error": task.Error}})
		return
	}
	defer browser.Close()

	if err := browser.Navigate(task.URL); err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("failed to navigate to %s: %v", task.URL, err)
		g.tasks.Save(task)
		g.events.Broadcast(task.ID, Event{Type: "error", Data: map[string]string{"error": task.Error}})
		return
	}
	browser.WaitLoad()

	maxSteps := g.cfg.AgentMaxSteps
	for stepIdx := 1; stepIdx <= maxSteps; stepIdx++ {
		select {
		case <-ctx.Done():
			task.Status = "failed"
			task.Error = "task cancelled"
			g.tasks.Save(task)
			g.events.Broadcast(task.ID, Event{Type: "status", Data: map[string]string{"status": "cancelled"}})
			return
		default:
		}

		screenshotFile := fmt.Sprintf("%s_step_%d.png", task.ID, stepIdx)
		screenshotPath := filepath.Join(g.tasks.ScreenshotDir(), screenshotFile)
		if err := browser.Screenshot(screenshotPath); err != nil {
			log.Printf("[agent] screenshot failed at step %d: %v", stepIdx, err)
		}

		elements, err := browser.ExtractDOMViaJS()
		if err != nil {
			log.Printf("[agent] DOM extraction failed at step %d: %v", stepIdx, err)
			elements = nil
		}

		currentURL := browser.CurrentURL()
		userPrompt := buildUserPrompt(task, elements, currentURL)

		g.events.Broadcast(task.ID, Event{Type: "thinking", Data: map[string]string{
			"step":    fmt.Sprintf("%d", stepIdx),
			"message": "Analyzing page and deciding next action...",
		}})

		action, err := g.callLLMWithRetry(ctx, task.Model, userPrompt, 2)
		if err != nil {
			step := Step{
				Index:      stepIdx,
				Action:     "error",
				Description: fmt.Sprintf("LLM call failed: %v", err),
				Screenshot: screenshotFile,
				Success:    false,
				Error:      err.Error(),
				Timestamp:  time.Now(),
			}
			task.Steps = append(task.Steps, step)
			task.Status = "failed"
			task.Error = err.Error()
			g.tasks.Save(task)
			g.events.Broadcast(task.ID, Event{Type: "step", Data: step})
			g.events.Broadcast(task.ID, Event{Type: "error", Data: map[string]string{"error": err.Error()}})
			return
		}

		step := Step{
			Index:       stepIdx,
			Thinking:    action.Thinking,
			Action:      action.Action,
			Selector:    action.Selector,
			Value:       action.Value,
			Description: action.Description,
			Screenshot:  screenshotFile,
			Success:     true,
			Timestamp:   time.Now(),
		}

		if action.Action == "done" {
			task.Steps = append(task.Steps, step)
			task.Status = "completed"
			g.tasks.Save(task)
			g.events.Broadcast(task.ID, Event{Type: "step", Data: step})
			g.events.Broadcast(task.ID, Event{Type: "status", Data: map[string]string{"status": "completed"}})
			return
		}

		execErr := g.executeAction(browser, action)
		if execErr != nil {
			step.Success = false
			step.Error = execErr.Error()
			log.Printf("[agent] action %q failed: %v", action.Action, execErr)
		}

		task.Steps = append(task.Steps, step)
		g.tasks.Save(task)
		g.events.Broadcast(task.ID, Event{Type: "step", Data: step})

		browser.WaitLoad()
	}

	task.Status = "completed"
	task.Error = "reached maximum steps"
	g.tasks.Save(task)
	g.events.Broadcast(task.ID, Event{Type: "status", Data: map[string]string{"status": "completed", "message": "max steps reached"}})
}

func (g *Gateway) executeAction(browser *BrowserSession, action AgentAction) error {
	switch action.Action {
	case "navigate":
		return browser.Navigate(action.Value)
	case "click":
		return browser.Click(action.Selector)
	case "type":
		return browser.Type(action.Selector, action.Value)
	case "scroll":
		return browser.Scroll(action.Value)
	case "select":
		return browser.SelectOption(action.Selector, action.Value)
	default:
		return fmt.Errorf("unknown action: %s", action.Action)
	}
}

func (g *Gateway) callLLMWithRetry(ctx context.Context, model, userPrompt string, maxRetries int) (AgentAction, error) {
	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		action, err := g.callLLM(ctx, model, userPrompt)
		if err == nil {
			return action, nil
		}
		lastErr = err
		if i < maxRetries {
			log.Printf("[agent] LLM call attempt %d failed, retrying: %v", i+1, err)
		}
	}
	return AgentAction{}, lastErr
}

func (g *Gateway) callLLM(ctx context.Context, model, userPrompt string) (AgentAction, error) {
	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}
	reqBody := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("http://localhost%s/v1/chat/completions", g.cfg.Listen)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return AgentAction{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return AgentAction{}, fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return AgentAction{}, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AgentAction{}, fmt.Errorf("failed to decode LLM response: %w", err)
	}
	if len(result.Choices) == 0 {
		return AgentAction{}, fmt.Errorf("LLM returned no choices")
	}

	content := result.Choices[0].Message.Content
	action, err := parseAgentAction(content)
	if err != nil {
		log.Printf("[agent] LLM raw response (len=%d): %s", len(content), content)
	}
	return action, err
}
