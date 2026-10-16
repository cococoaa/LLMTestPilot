package main

import (
	"net/http"
	"testing"
)

func TestSignDirect(t *testing.T) {
	p := &provider{cfg: ProviderConfig{APIKey: "sk-test"}}
	p.sign = p.signDirect
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	if err := p.sign(req, nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-test")
	}
}

func TestSignProxy(t *testing.T) {
	p := &provider{cfg: ProviderConfig{Headers: map[string]string{"X-Token": "abc", "X-App": "test"}}}
	p.sign = p.signProxy
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	if err := p.sign(req, nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Token"); got != "abc" {
		t.Errorf("X-Token = %q, want %q", got, "abc")
	}
	if got := req.Header.Get("X-App"); got != "test" {
		t.Errorf("X-App = %q, want %q", got, "test")
	}
}

func TestSignIChat(t *testing.T) {
	p := &provider{cfg: ProviderConfig{AppID: "app1", Secret: "secret1"}}
	p.sign = p.signIChat
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/v1/chat/completions", nil)
	body := []byte(`{"model":"test"}`)
	if err := p.sign(req, body); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-App-Id"); got != "app1" {
		t.Errorf("X-App-Id = %q, want %q", got, "app1")
	}
	if req.Header.Get("X-Timestamp") == "" {
		t.Error("X-Timestamp should not be empty")
	}
	if req.Header.Get("X-Signature") == "" {
		t.Error("X-Signature should not be empty")
	}
}

func TestSha256Hex(t *testing.T) {
	got := sha256Hex([]byte("hello"))
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("sha256Hex = %q, want %q", got, want)
	}
}
