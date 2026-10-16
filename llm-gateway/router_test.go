package main

import (
	"encoding/json"
	"testing"
)

func testConfig() *Config {
	return &Config{
		Providers: map[string]ProviderConfig{
			"adams":   {Type: "proxy", BaseURL: "http://adams.internal/v1"},
			"polaris": {Type: "direct", BaseURL: "https://api.deepseek.com/v1", APIKey: "test-key"},
		},
		Models: map[string]ModelEntry{
			"qwen3-max": {Provider: "adams", Model: "qwen3-max", UseVision: false, UseToolCalling: true},
			"deepseek":  {Provider: "polaris", Model: "deepseek-chat", UseVision: false, UseToolCalling: false},
		},
	}
}

func TestGetModel_Found(t *testing.T) {
	g := NewGateway(testConfig())
	mc, err := g.GetModel("qwen3-max")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.Provider != "adams" {
		t.Errorf("provider = %q, want %q", mc.Provider, "adams")
	}
	if mc.UpstreamModel != "qwen3-max" {
		t.Errorf("upstream model = %q, want %q", mc.UpstreamModel, "qwen3-max")
	}
	if !mc.UseToolCalling {
		t.Error("use_tool_calling should be true")
	}
}

func TestGetModel_NotFound(t *testing.T) {
	g := NewGateway(testConfig())
	_, err := g.GetModel("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestGetModel_UnknownProvider(t *testing.T) {
	cfg := testConfig()
	cfg.Models["broken"] = ModelEntry{Provider: "nonexistent", Model: "x"}
	g := NewGateway(cfg)
	_, err := g.GetModel("broken")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestReplaceModelInBody(t *testing.T) {
	body := []byte(`{"model":"original","messages":[{"role":"user","content":"hi"}],"stream":true,"tools":[{"type":"function"}],"top_p":0.9}`)
	got, err := replaceModelInBody(body, "new-model")
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}

	var model string
	if err := json.Unmarshal(m["model"], &model); err != nil {
		t.Fatal(err)
	}
	if model != "new-model" {
		t.Errorf("model = %q, want %q", model, "new-model")
	}

	for _, field := range []string{"messages", "stream", "tools", "top_p"} {
		if _, ok := m[field]; !ok {
			t.Errorf("field %q should be preserved", field)
		}
	}
}
