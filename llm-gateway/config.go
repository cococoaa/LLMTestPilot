package main

import (
	"encoding/json"
	"os"
)

type ProviderConfig struct {
	Type    string            `json:"type"`
	BaseURL string            `json:"base_url"`
	APIKey  string            `json:"api_key,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	AppID   string            `json:"app_id,omitempty"`
	Secret  string            `json:"secret,omitempty"`
}

type ModelEntry struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	UseVision      bool   `json:"use_vision"`
	UseToolCalling bool   `json:"use_tool_calling"`
}

type Config struct {
	Listen        string                    `json:"listen"`
	DataDir       string                    `json:"data_dir"`
	AgentMaxSteps int                       `json:"agent_max_steps"`
	Providers     map[string]ProviderConfig `json:"providers"`
	Models        map[string]ModelEntry     `json:"models"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = ":9090"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	if cfg.AgentMaxSteps == 0 {
		cfg.AgentMaxSteps = 20
	}
	return &cfg, nil
}
