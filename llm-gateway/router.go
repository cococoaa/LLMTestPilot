package main

import "fmt"

// ModelConfig 是路由后的模型装配结果，等价于原 Python 的 get_model() 返回值。
type ModelConfig struct {
	Provider       string
	UpstreamModel  string
	UseVision      bool
	UseToolCalling bool
}

// GetModel 根据对外模型别名路由到具体上游模型与提供方。
// 等价于原 Python 的 get_model() 巨型装配函数。
func (g *Gateway) GetModel(name string) (ModelConfig, error) {
	entry, ok := g.cfg.Models[name]
	if !ok {
		return ModelConfig{}, fmt.Errorf("unknown model: %s", name)
	}
	if _, ok := g.cfg.Providers[entry.Provider]; !ok {
		return ModelConfig{}, fmt.Errorf("unknown provider: %s", entry.Provider)
	}
	return ModelConfig{
		Provider:       entry.Provider,
		UpstreamModel:  entry.Model,
		UseVision:      entry.UseVision,
		UseToolCalling: entry.UseToolCalling,
	}, nil
}
