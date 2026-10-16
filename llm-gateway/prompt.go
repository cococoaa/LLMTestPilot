package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const systemPrompt = `You are a browser automation agent. Your task is to control a web browser to complete a testing task described by the user.

## Your Capabilities
- navigate: Go to a URL (put URL in "value")
- click: Click on an element (put CSS selector in "selector")
- type: Type text into an input field (put CSS selector in "selector", text in "value")
- scroll: Scroll the page (put "up" or "down" in "value")
- select: Select an option from a dropdown (put CSS selector in "selector", option value in "value")
- done: Mark the task as completed (put result summary in "description")

## Rules
1. Analyze the DOM elements list carefully before choosing an action.
2. Use the CSS selectors provided in the element list — they are guaranteed to work.
3. If the task goal is achieved, use "done" immediately.
4. If you cannot proceed after multiple attempts, use "done" and explain the failure.
5. Keep actions focused and efficient — no unnecessary steps.
6. For "type" action, the field will be cleared first, then your text will be entered.

## Response Format
Respond with ONLY a JSON object, no other text:
{"thinking":"your reasoning","action":"click","selector":"#btn","value":"","description":"Click the submit button"}`

type AgentAction struct {
	Thinking    string `json:"thinking"`
	Action      string `json:"action"`
	Selector    string `json:"selector"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

type DOMElement struct {
	Tag         string `json:"tag"`
	ID          string `json:"id,omitempty"`
	Class       string `json:"class,omitempty"`
	Text        string `json:"text,omitempty"`
	Type        string `json:"type,omitempty"`
	Href        string `json:"href,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Selector    string `json:"selector"`
}

func buildUserPrompt(task *Task, elements []DOMElement, currentURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task\n%s\n\n", task.Description)
	fmt.Fprintf(&b, "## Current URL\n%s\n\n", currentURL)

	if len(task.Steps) > 0 {
		b.WriteString("## Steps Already Taken\n")
		for _, s := range task.Steps {
			fmt.Fprintf(&b, "%d. [%s] %s", s.Index, s.Action, s.Description)
			if s.Error != "" {
				fmt.Fprintf(&b, " (ERROR: %s)", s.Error)
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	b.WriteString("## Interactive Elements on Current Page\n")
	if len(elements) == 0 {
		b.WriteString("No interactive elements found.\n")
	}
	limit := len(elements)
	if limit > 50 {
		limit = 50
	}
	for i := 0; i < limit; i++ {
		el := elements[i]
		fmt.Fprintf(&b, "[%d] <%s>", i+1, el.Tag)
		if el.ID != "" {
			fmt.Fprintf(&b, " id=%q", el.ID)
		}
		if el.Type != "" {
			fmt.Fprintf(&b, " type=%q", el.Type)
		}
		if el.Placeholder != "" {
			fmt.Fprintf(&b, " placeholder=%q", el.Placeholder)
		}
		if el.Text != "" {
			text := el.Text
			if len(text) > 60 {
				text = text[:60] + "..."
			}
			fmt.Fprintf(&b, " text=%q", text)
		}
		if el.Href != "" {
			href := el.Href
			if len(href) > 80 {
				href = href[:80] + "..."
			}
			fmt.Fprintf(&b, " href=%q", href)
		}
		fmt.Fprintf(&b, "  →  %s\n", el.Selector)
	}
	if len(elements) > 50 {
		fmt.Fprintf(&b, "... and %d more elements\n", len(elements)-50)
	}

	b.WriteString("\nRespond with JSON only. What is the next action?")
	return b.String()
}

var (
	jsonBlockRe = regexp.MustCompile(`(?s)` + "```" + `(?:json)?\s*(\{.*?\})\s*` + "```")
	thinkTagRe  = regexp.MustCompile(`(?s)<think>.*?</think>`)
)

func parseAgentAction(raw string) (AgentAction, error) {
	raw = strings.TrimSpace(raw)
	// Strip DeepSeek-style <think>...</think> tags
	cleaned := strings.TrimSpace(thinkTagRe.ReplaceAllString(raw, ""))
	if cleaned == "" {
		cleaned = raw
	}

	var action AgentAction
	if err := json.Unmarshal([]byte(cleaned), &action); err == nil && action.Action != "" {
		return action, nil
	}
	if m := jsonBlockRe.FindStringSubmatch(cleaned); len(m) > 1 {
		if err := json.Unmarshal([]byte(m[1]), &action); err == nil && action.Action != "" {
			return action, nil
		}
	}
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(cleaned[start:end+1]), &action); err == nil && action.Action != "" {
			return action, nil
		}
	}

	preview := raw
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return AgentAction{}, fmt.Errorf("failed to parse agent action from LLM response: %s", preview)
}
