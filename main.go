package main

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxy_plugin_call(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxy_plugin_free(void*, size_t);
extern void cliproxy_plugin_shutdown(void);
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const abiVersion = 1

const (
	pluginName       = "antigravity-coding-filter"
	pluginVersion    = "0.2.2"
	pluginRepository = "https://github.com/han-ava/cpa-plugin-antigravity-coding-filter"
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(_ *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	plugin.abi_version = abiVersion
	plugin.call = (C.cliproxy_plugin_call_fn)(C.cliproxy_plugin_call)
	plugin.free_buffer = (C.cliproxy_plugin_free_fn)(C.cliproxy_plugin_free)
	plugin.shutdown = (C.cliproxy_plugin_shutdown_fn)(C.cliproxy_plugin_shutdown)
	return 0
}

//export cliproxy_plugin_call
func cliproxy_plugin_call(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeCResponse(response, mustErrorEnvelope("invalid_method", "method is required"))
		return 1
	}

	methodName := C.GoString(method)
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}

	raw, code := handlePluginCall(methodName, requestBytes)
	writeCResponse(response, raw)
	return C.int(code)
}

//export cliproxy_plugin_free
func cliproxy_plugin_free(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxy_plugin_shutdown
func cliproxy_plugin_shutdown() {}

func writeCResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.malloc(C.size_t(len(raw)))
	if ptr == nil {
		return
	}
	C.memcpy(ptr, unsafe.Pointer(&raw[0]), C.size_t(len(raw)))
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

func handlePluginCall(method string, request []byte) ([]byte, int) {
	switch method {
	case pluginabi.MethodPluginRegister:
		return handlePluginLifecycle(request), 0
	case pluginabi.MethodPluginReconfigure:
		return handlePluginLifecycle(request), 0
	case pluginabi.MethodModelRoute:
		return handleModelRoute(request), 0
	case pluginabi.MethodExecutorExecute, pluginabi.MethodExecutorCountTokens, pluginabi.MethodExecutorExecuteStream:
		return blockErrorEnvelope(), 0
	case pluginabi.MethodExecutorHTTPRequest:
		return mustEnvelope(pluginapi.ExecutorHTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       blockPayload(),
			Headers:    jsonHeaders(),
		}), 0
	case pluginabi.MethodRequestInterceptBefore:
		return handleRequestInterceptBefore(request), 0
	case pluginabi.MethodRequestInterceptAfter:
		return mustEnvelope(pluginapi.RequestInterceptResponse{}), 0
	default:
		return mustErrorEnvelope("unknown_method", fmt.Sprintf("unknown method %q", method)), 0
	}
}

func handlePluginLifecycle(request []byte) []byte {
	if len(request) > 0 {
		cfg, err := filterConfigFromLifecycleRequest(request)
		if err != nil {
			return mustErrorEnvelope("invalid_config", err.Error())
		}
		applyFilterConfig(cfg)
	}
	return mustEnvelope(registrationResponse())
}

func registrationResponse() any {
	return struct {
		SchemaVersion uint32             `json:"schema_version"`
		Metadata      pluginapi.Metadata `json:"metadata"`
		Capabilities  struct {
			ModelRouter           bool                         `json:"model_router"`
			Executor              bool                         `json:"executor"`
			ExecutorModelScope    pluginapi.ExecutorModelScope `json:"executor_model_scope"`
			ExecutorInputFormats  []string                     `json:"executor_input_formats,omitempty"`
			ExecutorOutputFormats []string                     `json:"executor_output_formats,omitempty"`
			RequestInterceptor    bool                         `json:"request_interceptor"`
		} `json:"capabilities"`
	}{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           "local",
			GitHubRepository: pluginRepository,
			Logo:             "",
			ConfigFields:     configFields(),
		},
		Capabilities: struct {
			ModelRouter           bool                         `json:"model_router"`
			Executor              bool                         `json:"executor"`
			ExecutorModelScope    pluginapi.ExecutorModelScope `json:"executor_model_scope"`
			ExecutorInputFormats  []string                     `json:"executor_input_formats,omitempty"`
			ExecutorOutputFormats []string                     `json:"executor_output_formats,omitempty"`
			RequestInterceptor    bool                         `json:"request_interceptor"`
		}{
			ModelRouter:           true,
			Executor:              true,
			ExecutorModelScope:    pluginapi.ExecutorModelScopeBoth,
			ExecutorInputFormats:  []string{"chat-completions", "responses", "anthropic", "gemini"},
			ExecutorOutputFormats: []string{"chat-completions", "responses", "anthropic", "gemini"},
			RequestInterceptor:    true,
		},
	}
}

func configFields() []pluginapi.ConfigField {
	return []pluginapi.ConfigField{
		{
			Name:        "mode",
			Type:        pluginapi.ConfigFieldTypeEnum,
			EnumValues:  []string{string(filterModeBlock), string(filterModeRewrite)},
			Description: "How matched requests are handled: block rejects the request (default); rewrite replaces matched names with Antigravity.",
		},
		{
			Name:        "use_default_keywords",
			Type:        pluginapi.ConfigFieldTypeBoolean,
			Description: "Enable the built-in coding software and agent keyword preset.",
		},
		{
			Name:        "custom_mappings",
			Type:        pluginapi.ConfigFieldTypeObject,
			Description: "Additional case-insensitive system-field mappings. Keys are blocked in block mode and rewritten to their values in rewrite mode.",
		},
	}
}

func handleModelRoute(request []byte) []byte {
	var req pluginapi.ModelRouteRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return mustErrorEnvelope("invalid_request", fmt.Sprintf("decode model.route request: %v", err))
	}

	cfg := activeFilterConfig()
	if cfg.Mode != filterModeBlock {
		return mustEnvelope(pluginapi.ModelRouteResponse{Handled: false})
	}
	decision := classifyRequestWithConfig(req.Body, cfg)
	if !decision.Blocked {
		return mustEnvelope(pluginapi.ModelRouteResponse{Handled: false})
	}

	return mustEnvelope(pluginapi.ModelRouteResponse{
		Handled:    true,
		TargetKind: pluginapi.ModelRouteTargetSelf,
		Reason:     fmt.Sprintf("%s:%s", decision.Signal, decision.Detail),
	})
}

const (
	blockErrorCode    = "blocked_by_antigravity_coding_filter"
	blockErrorMessage = "request blocked because it matches a configured non-Antigravity coding software keyword"
)

func blockErrorEnvelope() []byte {
	return mustHTTPErrorEnvelope(blockErrorCode, blockErrorMessage, http.StatusForbidden)
}

func blockPayload() []byte {
	payload := map[string]any{
		"error": map[string]any{
			"code":    blockErrorCode,
			"message": blockErrorMessage,
			"type":    "invalid_request_error",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"error":{"code":"blocked_by_antigravity_coding_filter"}}`)
	}
	return raw
}

func jsonHeaders() http.Header {
	return http.Header{"content-type": []string{"application/json"}}
}

func handleRequestInterceptBefore(request []byte) []byte {
	var req pluginapi.RequestInterceptRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return mustErrorEnvelope("invalid_request", fmt.Sprintf("decode request.intercept_before request: %v", err))
	}
	cfg := activeFilterConfig()
	if cfg.Mode != filterModeRewrite {
		return mustEnvelope(pluginapi.RequestInterceptResponse{})
	}

	body, rewritten := rewriteRequestBodyWithConfig(req.Body, cfg)
	if !rewritten {
		return mustEnvelope(pluginapi.RequestInterceptResponse{})
	}
	return mustEnvelope(pluginapi.RequestInterceptResponse{Body: body})
}

func mustEnvelope(result any) []byte {
	raw, err := json.Marshal(pluginabi.Envelope{OK: true, Result: mustRawMessage(result)})
	if err != nil {
		return mustErrorEnvelope("marshal_error", err.Error())
	}
	return raw
}

func mustErrorEnvelope(code, message string) []byte {
	return mustHTTPErrorEnvelope(code, message, 0)
}

func mustHTTPErrorEnvelope(code, message string, status int) []byte {
	raw, err := json.Marshal(pluginabi.Envelope{OK: false, Error: &pluginabi.Error{
		Code:       code,
		Message:    message,
		HTTPStatus: status,
	}})
	if err != nil {
		return []byte(`{"ok":false,"error":{"code":"marshal_error","message":"failed to encode plugin response"}}`)
	}
	return raw
}

func mustRawMessage(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

var defaultRewriteMappings = []rewriteMapping{
	// Major AI code editors, assistants, and terminal coding agents.
	{Match: "Claude Code", Replacement: "Antigravity"},
	{Match: "OpenAI Codex", Replacement: "Antigravity"},
	{Match: "Codex CLI", Replacement: "Antigravity"},
	{Match: "Codex", Replacement: "Antigravity"},
	{Match: "OpenCode", Replacement: "Antigravity"},
	{Match: "GitHub Copilot CLI", Replacement: "Antigravity"},
	{Match: "GitHub Copilot", Replacement: "Antigravity"},
	{Match: "Gemini Code Assist", Replacement: "Antigravity"},
	{Match: "Gemini CLI", Replacement: "Antigravity"},
	{Match: "Cursor", Replacement: "Antigravity"},
	{Match: "Windsurf", Replacement: "Antigravity"},
	{Match: "Codeium", Replacement: "Antigravity"},
	{Match: "Cline", Replacement: "Antigravity"},
	{Match: "Roo Code", Replacement: "Antigravity"},
	{Match: "Kilo Code", Replacement: "Antigravity"},
	{Match: "Aider", Replacement: "Antigravity"},
	{Match: "Continue.dev", Replacement: "Antigravity"},
	{Match: "Amazon Q Developer", Replacement: "Antigravity"},
	{Match: "Amazon CodeWhisperer", Replacement: "Antigravity"},
	{Match: "JetBrains AI Assistant", Replacement: "Antigravity"},
	{Match: "JetBrains Junie", Replacement: "Antigravity"},
	{Match: "Kiro", Replacement: "Antigravity"},
	{Match: "Qoder CLI", Replacement: "Antigravity"},
	{Match: "Qoder", Replacement: "Antigravity"},
	{Match: "Qwen Code", Replacement: "Antigravity"},
	{Match: "Trae", Replacement: "Antigravity"},
	{Match: "Tabnine", Replacement: "Antigravity"},
	{Match: "Sourcegraph Cody", Replacement: "Antigravity"},
	{Match: "Augment Code", Replacement: "Antigravity"},
	{Match: "Replit Agent", Replacement: "Antigravity"},
	{Match: "Replit Ghostwriter", Replacement: "Antigravity"},
	{Match: "Devin", Replacement: "Antigravity"},
	{Match: "OpenHands", Replacement: "Antigravity"},
	{Match: "SWE-agent", Replacement: "Antigravity"},
	{Match: "Goose", Replacement: "Antigravity"},
	{Match: "Zed AI", Replacement: "Antigravity"},
	{Match: "Void Editor", Replacement: "Antigravity"},
	{Match: "PearAI", Replacement: "Antigravity"},
	{Match: "Refact.ai", Replacement: "Antigravity"},
	{Match: "Tabby", Replacement: "Antigravity"},
	{Match: "GitLab Duo", Replacement: "Antigravity"},
	{Match: "Visual Studio IntelliCode", Replacement: "Antigravity"},
	{Match: "CodeBuddy", Replacement: "Antigravity"},
	{Match: "Blackbox AI", Replacement: "Antigravity"},
	{Match: "Pieces for Developers", Replacement: "Antigravity"},
	{Match: "Qodo", Replacement: "Antigravity"},
	{Match: "CodiumAI", Replacement: "Antigravity"},
	{Match: "Rovo Dev CLI", Replacement: "Antigravity"},
	{Match: "Factory Droid", Replacement: "Antigravity"},

	// General-purpose local agents that can generate and modify code.
	{Match: "OpenClaw", Replacement: "Antigravity"},
	{Match: "Clawdbot", Replacement: "Antigravity"},
	{Match: "Moltbot", Replacement: "Antigravity"},
	{Match: "Hermes Agent", Replacement: "Antigravity"},
	{Match: "Hermes", Replacement: "Antigravity"},
	{Match: "WorkBuddy", Replacement: "Antigravity"},
}

type filterMode string

const (
	filterModeBlock   filterMode = "block"
	filterModeRewrite filterMode = "rewrite"
)

type rewriteMapping struct {
	Match       string
	Replacement string
}

type filterConfig struct {
	Mode               filterMode
	UseDefaultKeywords bool
	CustomMappings     []rewriteMapping
}

var (
	filterConfigMu      sync.RWMutex
	currentFilterConfig = defaultFilterConfig()
)

func defaultFilterConfig() filterConfig {
	return filterConfig{Mode: filterModeBlock, UseDefaultKeywords: true}
}

func applyFilterConfig(cfg filterConfig) {
	filterConfigMu.Lock()
	defer filterConfigMu.Unlock()

	currentFilterConfig = filterConfig{
		Mode:               cfg.Mode,
		UseDefaultKeywords: cfg.UseDefaultKeywords,
		CustomMappings:     append([]rewriteMapping(nil), normalizeMappings(cfg.CustomMappings)...),
	}
}

func activeFilterConfig() filterConfig {
	filterConfigMu.RLock()
	defer filterConfigMu.RUnlock()

	return filterConfig{
		Mode:               currentFilterConfig.Mode,
		UseDefaultKeywords: currentFilterConfig.UseDefaultKeywords,
		CustomMappings:     append([]rewriteMapping(nil), currentFilterConfig.CustomMappings...),
	}
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

func filterConfigFromLifecycleRequest(request []byte) (filterConfig, error) {
	var req lifecycleRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return filterConfig{}, fmt.Errorf("decode lifecycle request: %w", err)
	}
	return parseFilterConfigYAML(req.ConfigYAML)
}

func parseFilterConfigYAML(raw []byte) (filterConfig, error) {
	cfg := defaultFilterConfig()
	if len(strings.TrimSpace(string(raw))) == 0 {
		return cfg, nil
	}

	var values map[string]any
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return filterConfig{}, fmt.Errorf("decode config yaml: %w", err)
	}
	if value, exists := values["mode"]; exists {
		text, ok := value.(string)
		if !ok {
			return filterConfig{}, fmt.Errorf("mode must be a string")
		}
		cfg.Mode = filterMode(strings.ToLower(strings.TrimSpace(text)))
		if cfg.Mode != filterModeBlock && cfg.Mode != filterModeRewrite {
			return filterConfig{}, fmt.Errorf("mode must be one of: block, rewrite")
		}
	}
	if value, exists := values["use_default_keywords"]; exists {
		boolValue, ok := value.(bool)
		if !ok {
			return filterConfig{}, fmt.Errorf("use_default_keywords must be a boolean")
		}
		cfg.UseDefaultKeywords = boolValue
	}
	if value, exists := values["custom_mappings"]; exists {
		mappings, err := parseCustomMappings(value)
		if err != nil {
			return filterConfig{}, err
		}
		cfg.CustomMappings = mappings
	}
	return cfg, nil
}

func parseCustomMappings(value any) ([]rewriteMapping, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return parseMappingString(typed)
	case map[string]any:
		mappings := make([]rewriteMapping, 0, len(typed))
		for match, replacement := range typed {
			text, ok := replacement.(string)
			if !ok {
				return nil, fmt.Errorf("custom_mappings values must be strings")
			}
			mappings = append(mappings, rewriteMapping{Match: match, Replacement: text})
		}
		return mappings, nil
	case []any:
		mappings := make([]rewriteMapping, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("custom_mappings entries must be strings")
			}
			parsed, err := parseMappingString(text)
			if err != nil {
				return nil, err
			}
			mappings = append(mappings, parsed...)
		}
		return mappings, nil
	default:
		return nil, fmt.Errorf("custom_mappings must be an object, array, or string")
	}
}

func parseMappingString(value string) ([]rewriteMapping, error) {
	entries := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	mappings := make([]rewriteMapping, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		match, replacement, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("custom_mappings entries must use match: replacement")
		}
		mappings = append(mappings, rewriteMapping{Match: match, Replacement: replacement})
	}
	return mappings, nil
}

func effectiveMappings(cfg filterConfig) []rewriteMapping {
	mappings := make([]rewriteMapping, 0, len(defaultRewriteMappings)+len(cfg.CustomMappings))
	if cfg.UseDefaultKeywords {
		mappings = append(mappings, defaultRewriteMappings...)
	}
	mappings = append(mappings, cfg.CustomMappings...)
	return normalizeMappings(mappings)
}

func normalizeMappings(mappings []rewriteMapping) []rewriteMapping {
	seen := make(map[string]struct{}, len(mappings))
	reversed := make([]rewriteMapping, 0, len(mappings))
	for i := len(mappings) - 1; i >= 0; i-- {
		match := strings.ToLower(strings.TrimSpace(mappings[i].Match))
		replacement := strings.TrimSpace(mappings[i].Replacement)
		if match == "" || replacement == "" {
			continue
		}
		if _, exists := seen[match]; exists {
			continue
		}
		seen[match] = struct{}{}
		reversed = append(reversed, rewriteMapping{Match: match, Replacement: replacement})
	}
	out := make([]rewriteMapping, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		out = append(out, reversed[i])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Match) == len(out[j].Match) {
			return out[i].Match < out[j].Match
		}
		return len(out[i].Match) > len(out[j].Match)
	})
	return out
}

type filterDecision struct {
	Blocked bool
	Signal  string
	Detail  string
}

func classifyRequest(body []byte) filterDecision {
	return classifyRequestWithConfig(body, activeFilterConfig())
}

func classifyRequestWithConfig(body []byte, cfg filterConfig) filterDecision {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return filterDecision{}
	}
	for _, mapping := range effectiveMappings(cfg) {
		if _, changed := rewriteSystemFields(root, []rewriteMapping{mapping}); changed {
			return filterDecision{Blocked: true, Signal: "system.keyword", Detail: mapping.Match}
		}
	}
	return filterDecision{}
}

func rewriteRequestBody(body []byte) ([]byte, bool) {
	return rewriteRequestBodyWithConfig(body, activeFilterConfig())
}

func rewriteRequestBodyWithConfig(body []byte, cfg filterConfig) ([]byte, bool) {
	var root any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if !json.Valid(body) {
		return nil, false
	}
	if err := decoder.Decode(&root); err != nil {
		return nil, false
	}
	rewritten, changed := rewriteSystemFields(root, effectiveMappings(cfg))
	if !changed {
		return nil, false
	}
	raw, err := json.Marshal(rewritten)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// Only the Anthropic top-level system field is supported. Never traverse tools,
// metadata or user content looking for arbitrary properties named system.
func rewriteSystemFields(value any, mappings []rewriteMapping) (any, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return value, false
	}
	next, changed := rewriteSystemValue(root["system"], mappings)
	if changed {
		root["system"] = next
	}
	return value, changed
}

func rewriteSystemValue(value any, mappings []rewriteMapping) (any, bool) {
	switch typed := value.(type) {
	case string:
		next := typed
		changed := false
		for _, mapping := range mappings {
			var replaced bool
			next, replaced = replaceInsensitive(next, mapping.Match, mapping.Replacement)
			changed = changed || replaced
		}
		return next, changed
	case []any:
		changed := false
		for _, child := range typed {
			block, ok := child.(map[string]any)
			if !ok || block["type"] != "text" {
				continue
			}
			text, ok := block["text"].(string)
			if !ok {
				continue
			}
			next, replaced := rewriteSystemValue(text, mappings)
			if replaced {
				block["text"] = next
				changed = true
			}
		}
		return value, changed
	default:
		return value, false
	}
}

func replaceInsensitive(value, match, replacement string) (string, bool) {
	needle := []rune(strings.ToLower(match))
	if len(needle) == 0 {
		return value, false
	}
	runes := []rune(value)
	var out strings.Builder
	changed := false
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }
	for i := 0; i < len(runes); {
		end := i + len(needle)
		found := end <= len(runes)
		if found {
			for j, r := range needle {
				if unicode.ToLower(runes[i+j]) != r {
					found = false
					break
				}
			}
		}
		if found && ((i > 0 && word(runes[i-1])) || (end < len(runes) && word(runes[end]))) {
			found = false
		}
		// Ambiguous product names require an explicit identity statement. Ordinary
		// technical prose (e.g. cursor pagination) must retain its original meaning.
		if found {
			switch strings.ToLower(match) {
			case "cursor", "cline", "aider", "goose", "tabby", "hermes", "trae", "devin":
				prefix := strings.ToLower(strings.TrimSpace(string(runes[:i])))
				suffix := strings.ToLower(string(runes[end:]))
				found = strings.HasSuffix(prefix, "you are") || strings.HasSuffix(prefix, "run as") || strings.HasPrefix(suffix, " session")
			}
		}
		if found {
			out.WriteString(replacement)
			i = end
			changed = true
		} else {
			out.WriteRune(runes[i])
			i++
		}
	}
	if !changed {
		return value, false
	}
	return out.String(), true
}

func walkJSON(value any, visit func(path []string, value any) bool) {
	var walk func(path []string, current any) bool
	walk = func(path []string, current any) bool {
		if !visit(path, current) {
			return false
		}
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if !walk(appendPath(path, key), child) {
					return false
				}
			}
		case []any:
			for index, child := range typed {
				if !walk(appendPath(path, fmt.Sprintf("%d", index)), child) {
					return false
				}
			}
		}
		return true
	}
	walk(nil, value)
}

func appendPath(path []string, item string) []string {
	next := make([]string, len(path), len(path)+1)
	copy(next, path)
	return append(next, item)
}

func collectText(value any) string {
	var parts []string
	var collect func(any)
	collect = func(current any) {
		switch typed := current.(type) {
		case string:
			parts = append(parts, typed)
		case map[string]any:
			for _, child := range typed {
				collect(child)
			}
		case []any:
			for _, child := range typed {
				collect(child)
			}
		}
	}
	collect(value)
	return strings.Join(parts, "\n")
}
