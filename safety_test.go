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
