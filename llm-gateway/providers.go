package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Provider 抽象所有上游模型服务的统一调用接口。
type Provider interface {
	Complete(ctx context.Context, body []byte) (*http.Response, error)
}

type provider struct {
	cfg    ProviderConfig
	client *http.Client
	sign   func(r *http.Request, body []byte) error
}

func newProvider(cfg ProviderConfig) Provider {
	p := &provider{
		cfg: cfg,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
	switch cfg.Type {
	case "ichat":
		p.sign = p.signIChat
	case "proxy":
		p.sign = p.signProxy
	default:
		p.sign = p.signDirect
	}
	return p
}

func (p *provider) Complete(ctx context.Context, body []byte) (*http.Response, error) {
	url := p.cfg.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err := p.sign(httpReq, body); err != nil {
		return nil, err
	}
	return p.client.Do(httpReq)
}

func (p *provider) signDirect(r *http.Request, _ []byte) error {
	r.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	return nil
}

func (p *provider) signProxy(r *http.Request, _ []byte) error {
	for k, v := range p.cfg.Headers {
		r.Header.Set(k, v)
	}
	return nil
}

// signIChat 网关方式：HMAC-SHA256 实时签名。
// 签名串 = method \n path \n timestamp \n sha256(body)
func (p *provider) signIChat(r *http.Request, body []byte) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s", r.Method, r.URL.Path, ts, sha256Hex(body))
	mac := hmac.New(sha256.New, []byte(p.cfg.Secret))
	mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))
	r.Header.Set("X-App-Id", p.cfg.AppID)
	r.Header.Set("X-Timestamp", ts)
	r.Header.Set("X-Signature", sig)
	return nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
