package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeRewriteScope(t *testing.T) {
	for _, body := range []string{
		`{"system":"Use cursor pagination and a precodex index."}`,
		`{"tools":[{"input_schema":{"properties":{"system":{"description":"Claude Code"}}}}]}`,
		`{"system":[{"type":"image","source":{"data":"Cursor"}},{"type":"text","text":"plain","signature":"Claude Code"}]}`,
		`{"messages":[{"role":"user","content":"Claude Code"}]}`,
	} {
		if got, changed := rewriteRequestBody([]byte(body)); changed {
			t.Fatalf("unexpected rewrite: %s", got)
		}
		if classifyRequest([]byte(body)).Blocked {
			t.Fatalf("unexpected block: %s", body)
		}
	}
}

func TestUnicodeAndNumberPreservation(t *testing.T) {
	got, changed := rewriteRequestBody([]byte(`{"system":"İ You are Cursor.","seed":9007199254740993}`))
	if !changed || !json.Valid(got) || !strings.Contains(string(got), `İ You are Antigravity.`) || !strings.Contains(string(got), `9007199254740993`) {
		t.Fatalf("corrupted payload: %s", got)
	}
}

func TestRewriteKeepsModelAndMetadata(t *testing.T) {
	got, changed := rewriteRequestBody([]byte(`{"model":"gemini-3.8-flash-high","system":[{"type":"text","text":"You are Claude Code.","signature":"Cursor"}],"metadata":{"system":"Claude Code"}}`))
	if !changed {
		t.Fatal("expected rewrite")
	}
	var d map[string]any
	json.Unmarshal(got, &d)
	if d["model"] != "gemini-3.8-flash-high" || d["metadata"].(map[string]any)["system"] != "Claude Code" || d["system"].([]any)[0].(map[string]any)["signature"] != "Cursor" {
		t.Fatalf("unexpected change: %s", got)
	}
}

func TestProviderIsolation(t *testing.T) {
	defer restoreDefaultFilterConfig(t)
	for _, mode := range []filterMode{filterModeRewrite, filterModeBlock} {
		applyFilterConfig(filterConfig{Mode: mode, UseDefaultKeywords: true})
		for _, target := range []string{"", "codex", "openai", "claude", "gemini", "antigravity"} {
			for _, method := range []string{"request.intercept_before", "request.intercept_after", "model.route"} {
				request, _ := json.Marshal(map[string]any{"ToFormat": target, "Model": "gemini-3.8-flash-high", "RequestedModel": "antigravity/test", "Body": []byte(`{"system":"You are Codex."}`)})
				raw, _ := handlePluginCall(method, request)
				var r struct {
					Result struct {
						Body       []byte
						Terminate  bool
						StatusCode int
						Handled    bool
					}
				}
				json.Unmarshal(raw, &r)
				applies := target == "antigravity" && method == "request.intercept_after"
				if r.Result.Handled || (len(r.Result.Body) > 0) != (applies && mode == filterModeRewrite) || r.Result.Terminate != (applies && mode == filterModeBlock) {
					t.Fatalf("mode=%s target=%s method=%s: %s", mode, target, method, raw)
				}
				if r.Result.Terminate && r.Result.StatusCode != 403 {
					t.Fatalf("wrong block status: %s", raw)
				}
			}
		}
	}
}
