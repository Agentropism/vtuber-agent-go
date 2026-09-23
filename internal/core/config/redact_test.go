package config

import (
	"testing"
	"time"
)

// 脱敏是「凭据只在环境变量或 config.toml 里、不进仓库、不出进程」这条约定的最后一道门：
// 密钥字段必须只回 configured 布尔，非密钥字段（尤其 cookie_file 这种路径）必须照常回值。
func TestRedactedMapHidesSecrets(t *testing.T) {
	var cfg Config

	cfg.LLM.APIKey = "sk-secret"
	cfg.LLM.BaseURL = "https://api.example.com/v1"
	cfg.Stream.Cookie = "SESSDATA=xxx"
	cfg.Stream.CookieFile = "data/bilibili-cookie.txt"
	cfg.TTS.OpenAI.APIKey = "   " // 只有空白也算没配

	got := RedactedMap(&cfg)

	llm, ok := got["llm"].(map[string]any)
	if !ok {
		t.Fatalf("llm 段类型不对: %T", got["llm"])
	}
	if _, exists := llm["api_key"].(map[string]bool); !exists {
		t.Errorf("api_key 应回 configured 形状，实际 %#v", llm["api_key"])
	}
	if configured := llm["api_key"].(map[string]bool)["configured"]; !configured {
		t.Error("有值的 api_key 应标记为已配置")
	}
	if llm["base_url"] != "https://api.example.com/v1" {
		t.Errorf("非密钥字段应照常回值，实际 %#v", llm["base_url"])
	}

	stream, ok := got["stream"].(map[string]any)
	if !ok {
		t.Fatalf("stream 段类型不对: %T", got["stream"])
	}
	if stream["cookie_file"] != "data/bilibili-cookie.txt" {
		t.Errorf("cookie_file 是路径不是凭据，应照常回值，实际 %#v", stream["cookie_file"])
	}

	tts, ok := got["tts"].(map[string]any)
	if !ok {
		t.Fatalf("tts 段类型不对: %T", got["tts"])
	}
	openai, ok := tts["openai_tts"].(map[string]any)
	if !ok {
		t.Fatalf("tts.openai_tts 段类型不对: %T", tts["openai_tts"])
	}
	if configured := openai["api_key"].(map[string]bool)["configured"]; configured {
		t.Error("只有空白的 api_key 应算未配置")
	}
}

// 时间段要渲染成 "5m0s" 这样的字符串，而不是纳秒整数：这个快照是给人看、给前端显示的。
func TestRedactedMapRendersDurations(t *testing.T) {
	var cfg Config
	cfg.Agent.TurnTimeout = Duration(60 * time.Second)
	cfg.Agent.IdleSpeakInterval = Duration(5 * time.Minute)

	got := RedactedMap(&cfg)

	agent, ok := got["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent 段类型不对: %T", got["agent"])
	}
	if agent["turn_timeout"] != "1m0s" {
		t.Errorf("turn_timeout = %#v, want \"1m0s\"", agent["turn_timeout"])
	}
	if agent["idle_speak_interval"] != "5m0s" {
		t.Errorf("idle_speak_interval = %#v, want \"5m0s\"", agent["idle_speak_interval"])
	}
}

// 引擎列表是切片，要按顺序原样出现（降级顺序是有意义的配置）。
func TestRedactedMapKeepsSliceOrder(t *testing.T) {
	var cfg Config
	cfg.TTS.Engines = []string{"edge_tts", "openai_tts"}

	got := RedactedMap(&cfg)

	tts, ok := got["tts"].(map[string]any)
	if !ok {
		t.Fatalf("tts 段类型不对: %T", got["tts"])
	}
	engines, ok := tts["engines"].([]any)
	if !ok {
		t.Fatalf("engines 类型不对: %T", tts["engines"])
	}
	if len(engines) != 2 || engines[0] != "edge_tts" || engines[1] != "openai_tts" {
		t.Errorf("engines 顺序或内容不对: %#v", engines)
	}
}

// nil 配置不该 panic：调用方（config show / api）在配置不可用时仍要给出可读结果。
func TestRedactedMapNil(t *testing.T) {
	if got := RedactedMap(nil); got != nil {
		t.Errorf("nil 配置应回 nil，实际 %#v", got)
	}
}
