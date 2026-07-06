package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestOllamaCloudExecutorRoutesChatToOpenAICompatibleV1(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-oss:120b","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	exec := NewOllamaCloudExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL}}
	resp, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-oss:120b",
		Payload: []byte(`{"model":"gpt-oss:120b","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotText := gjson.GetBytes(resp.Payload, "choices.0.message.content").String(); gotText != "ok" {
		t.Fatalf("response text = %q, want ok; payload=%s", gotText, string(resp.Payload))
	}
}

func TestOllamaCloudExecutorRoutesResponsesToOfficialEndpoint(t *testing.T) {
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1,"model":"gpt-oss:120b","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	exec := NewOllamaCloudExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL}}
	resp, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-oss:120b",
		Payload: []byte(`{"model":"client-prefix/gpt-oss:120b","input":"hi"}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if gotModel := gjson.GetBytes(gotBody, "model").String(); gotModel != "gpt-oss:120b" {
		t.Fatalf("upstream model = %q, want gpt-oss:120b; body=%s", gotModel, string(gotBody))
	}
	if gotText := gjson.GetBytes(resp.Payload, "output.0.content.0.text").String(); gotText != "ok" {
		t.Fatalf("response text = %q, want ok; payload=%s", gotText, string(resp.Payload))
	}
}

func TestOllamaCloudExecutorStreamsResponsesFromOfficialEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.created","response":{"id":"resp_1"}}` + "\n\n"))
	}))
	defer server.Close()

	exec := NewOllamaCloudExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL}}
	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-oss:120b",
		Payload: []byte(`{"model":"gpt-oss:120b","input":"hi","stream":true}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err != nil {
		t.Fatalf("ExecuteStream returned error: %v", err)
	}
	var chunks strings.Builder
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		chunks.Write(chunk.Payload)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if got := chunks.String(); got != "event: response.created\n"+`data: {"type":"response.created","response":{"id":"resp_1"}}`+"\n\n" {
		t.Fatalf("stream payload = %q", got)
	}
}

func TestOllamaCloudExecutorRoutesClaudeMessagesToOfficialEndpoint(t *testing.T) {
	var gotPath, gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotVersion = r.Header.Get("Anthropic-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"gpt-oss:120b","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	exec := NewOllamaCloudExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL}}
	resp, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-oss:120b",
		Payload: []byte(`{"model":"gpt-oss:120b","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotVersion != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q, want 2023-06-01", gotVersion)
	}
	if gotText := gjson.GetBytes(resp.Payload, "content.0.text").String(); gotText != "ok" {
		t.Fatalf("response text = %q, want ok; payload=%s", gotText, string(resp.Payload))
	}
}
