package main

import (
	"testing"
)

func TestParseAgentAction_ValidJSON(t *testing.T) {
	raw := `{"thinking":"need to click","action":"click","selector":"#btn","value":"","description":"Click submit"}`
	a, err := parseAgentAction(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Action != "click" {
		t.Errorf("expected action 'click', got %q", a.Action)
	}
	if a.Selector != "#btn" {
		t.Errorf("expected selector '#btn', got %q", a.Selector)
	}
}

func TestParseAgentAction_MarkdownWrapped(t *testing.T) {
	raw := "Sure, here's the action:\n```json\n{\"thinking\":\"scroll down\",\"action\":\"scroll\",\"selector\":\"\",\"value\":\"down\",\"description\":\"Scroll page down\"}\n```"
	a, err := parseAgentAction(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Action != "scroll" {
		t.Errorf("expected action 'scroll', got %q", a.Action)
	}
}

func TestParseAgentAction_WithExtraText(t *testing.T) {
	raw := "I think we should navigate. {\"thinking\":\"go to page\",\"action\":\"navigate\",\"selector\":\"\",\"value\":\"https://example.com\",\"description\":\"Navigate to example\"} That should work."
	a, err := parseAgentAction(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Action != "navigate" {
		t.Errorf("expected action 'navigate', got %q", a.Action)
	}
	if a.Value != "https://example.com" {
		t.Errorf("expected value 'https://example.com', got %q", a.Value)
	}
}

func TestParseAgentAction_ThinkTag(t *testing.T) {
	raw := `<think>
The page has a search box. I need to type in it.
Let me use the input field with id "search".
</think>
{"thinking":"type in search box","action":"type","selector":"#search","value":"hello","description":"Type hello in search"}`
	a, err := parseAgentAction(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Action != "type" {
		t.Errorf("expected action 'type', got %q", a.Action)
	}
	if a.Value != "hello" {
		t.Errorf("expected value 'hello', got %q", a.Value)
	}
}

func TestParseAgentAction_Invalid(t *testing.T) {
	_, err := parseAgentAction("This is not JSON at all")
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestBuildUserPrompt(t *testing.T) {
	task := &Task{
		Description: "Search for hello on Google",
		Steps: []Step{
			{Index: 1, Action: "navigate", Description: "Go to google.com"},
		},
	}
	elements := []DOMElement{
		{Tag: "input", ID: "search", Type: "text", Placeholder: "Search", Selector: "#search"},
		{Tag: "button", Text: "Google Search", Selector: "button"},
	}
	prompt := buildUserPrompt(task, elements, "https://www.google.com")
	if len(prompt) == 0 {
		t.Fatal("prompt should not be empty")
	}
	if !contains(prompt, "Search for hello") {
		t.Error("prompt should contain task description")
	}
	if !contains(prompt, "#search") {
		t.Error("prompt should contain element selector")
	}
	if !contains(prompt, "Steps Already Taken") {
		t.Error("prompt should contain previous steps")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
