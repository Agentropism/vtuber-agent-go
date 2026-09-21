package tts

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capturedRequest 记录假端点收到的一次请求。
type capturedRequest struct {
	path   string
	auth   string
	query  map[string][]string
	body   map[string]any
	header http.Header
}

func TestOpenAICompatibleEngineRequestShape(t *testing.T) {
	var got capturedRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = capture(r)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{1, 2, 3, 4})
	}))
	defer server.Close()

	engine, err := NewOpenAICompatibleEngine(OpenAICompatibleConfig{
		Name:    "openai_tts",
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "tts-1",
		Voice:   "alloy",
		Speed:   1.2,
	})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}

	pcm, err := engine.Synthesize(context.Background(), "你好")
	if err != nil {
		t.Fatalf("合成失败: %v", err)
	}
	if len(pcm) != 4 {
		t.Errorf("返回字节数不符合预期: %d", len(pcm))
	}

	if got.path != "/v1/audio/speech" {
		t.Errorf("请求路径不符合预期: %q", got.path)
	}
	if got.auth != "Bearer sk-test" {
		t.Errorf("鉴权头不符合预期: %q", got.auth)
	}
	if got.body["model"] != "tts-1" || got.body["voice"] != "alloy" || got.body["input"] != "你好" {
		t.Errorf("请求体关键字段不符合预期: %+v", got.body)
	}
	if got.body["response_format"] != responseFormatPCM {
		t.Errorf("应索要 PCM 格式，实际 %v", got.body["response_format"])
	}
	if got.body["speed"] != 1.2 {
		t.Errorf("语速未下发: %v", got.body["speed"])
	}
	// 未配置 sample_rate / gain 时不应出现在请求体里
	if _, exists := got.body["sample_rate"]; exists {
		t.Errorf("未配置时不应下发 sample_rate: %+v", got.body)
	}
}

func TestOpenAICompatibleEngineSiliconflowExtras(t *testing.T) {
	var got capturedRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = capture(r)
		_, _ = w.Write([]byte{1, 2})
	}))
	defer server.Close()

	engine, err := NewOpenAICompatibleEngine(OpenAICompatibleConfig{
		Name: "siliconflow_tts", BaseURL: server.URL + "/v1", APIKey: "k",
		Model: "FunAudioLLM/CosyVoice2-0.5B", Voice: "speech:Dreamflowers:5bdstvc39i:xkqldnpasqmoqbakubom",
		SampleRate: SampleRate, Gain: 3,
	})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}
	if _, err := engine.Synthesize(context.Background(), "你好"); err != nil {
		t.Fatalf("合成失败: %v", err)
	}

	if got.body["sample_rate"] != float64(SampleRate) {
		t.Errorf("sample_rate 未下发: %v", got.body["sample_rate"])
	}
	if got.body["gain"] != float64(3) {
		t.Errorf("gain 未下发: %v", got.body["gain"])
	}
}

func TestOpenAICompatibleEngineResamplesNonContractRate(t *testing.T) {
	// 端点声明 48kHz：返回 480 点应被重采样成 240 点
	pcm48k := make([]byte, 480*2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pcm48k)
	}))
	defer server.Close()

	engine, err := NewOpenAICompatibleEngine(OpenAICompatibleConfig{
		Name: "resampling", BaseURL: server.URL + "/v1", APIKey: "k",
		Model: "m", Voice: "v", NativeSampleRate: 48000,
	})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}

	got, err := engine.Synthesize(context.Background(), "你好")
	if err != nil {
		t.Fatalf("合成失败: %v", err)
	}
	if len(got)/2 != 240 {
		t.Errorf("重采样后点数不符合预期: %d", len(got)/2)
	}
}

func TestOpenAICompatibleEngineSurfacesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	engine, err := NewOpenAICompatibleEngine(OpenAICompatibleConfig{
		Name: "failing", BaseURL: server.URL + "/v1", APIKey: "bad", Model: "m", Voice: "v",
	})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}

	_, err = engine.Synthesize(context.Background(), "你好")
	if err == nil {
		t.Fatal("HTTP 401 应返回错误")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("错误应包含状态码与响应片段: %v", err)
	}
}

func TestFishEngineRequestShape(t *testing.T) {
	var got capturedRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = capture(r)
		_, _ = w.Write([]byte{7, 7, 7})
	}))
	defer server.Close()

	engine, err := NewFishEngine(FishConfig{
		BaseURL: server.URL, APIKey: "fish-key",
		ReferenceID: "ref-1", Latency: "balanced",
	})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}
	if _, err := engine.Synthesize(context.Background(), "你好"); err != nil {
		t.Fatalf("合成失败: %v", err)
	}

	if got.path != "/v1/tts" {
		t.Errorf("请求路径不符合预期: %q", got.path)
	}
	if got.auth != "Bearer fish-key" {
		t.Errorf("鉴权头不符合预期: %q", got.auth)
	}
	if got.body["format"] != responseFormatPCM {
		t.Errorf("应索要 PCM 格式，实际 %v", got.body["format"])
	}
	if got.body["sample_rate"] != float64(SampleRate) {
		t.Errorf("sample_rate 未下发: %v", got.body["sample_rate"])
	}
	if got.body["reference_id"] != "ref-1" || got.body["latency"] != "balanced" {
		t.Errorf("音色参数不符合预期: %+v", got.body)
	}
}

func TestMinimaxEngineDecodesHexAudio(t *testing.T) {
	var got capturedRequest
	audio := []byte{1, 2, 3, 4, 5, 6}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = capture(r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString(audio)},
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
		})
	}))
	defer server.Close()

	engine, err := NewMinimaxEngine(MinimaxConfig{
		BaseURL: server.URL, GroupID: "gid-1", APIKey: "mm-key",
	})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}

	pcm, err := engine.Synthesize(context.Background(), "你好")
	if err != nil {
		t.Fatalf("合成失败: %v", err)
	}
	if string(pcm) != string(audio) {
		t.Errorf("hex 解码结果不符合预期: %v", pcm)
	}

	if got.path != "/v1/t2a_v2" {
		t.Errorf("请求路径不符合预期: %q", got.path)
	}
	if values := got.query["GroupId"]; len(values) != 1 || values[0] != "gid-1" {
		t.Errorf("GroupId 查询参数不符合预期: %v", got.query)
	}
	if got.auth != "Bearer mm-key" {
		t.Errorf("鉴权头不符合预期: %q", got.auth)
	}
	if got.body["stream"] != false {
		t.Errorf("应使用非流式请求: %v", got.body["stream"])
	}

	audioSetting, _ := got.body["audio_setting"].(map[string]any)
	if audioSetting["format"] != responseFormatPCM {
		t.Errorf("应索要 PCM 格式，实际 %v", audioSetting["format"])
	}
	if audioSetting["sample_rate"] != float64(SampleRate) {
		t.Errorf("采样率不符合预期: %v", audioSetting["sample_rate"])
	}
}

func TestMinimaxEngineSurfacesBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_resp": map[string]any{"status_code": 1004, "status_msg": "invalid api key"},
		})
	}))
	defer server.Close()

	engine, err := NewMinimaxEngine(MinimaxConfig{BaseURL: server.URL, GroupID: "g", APIKey: "k"})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}

	_, err = engine.Synthesize(context.Background(), "你好")
	if err == nil {
		t.Fatal("业务错误码应返回错误")
	}
	if !strings.Contains(err.Error(), "1004") {
		t.Errorf("错误应包含业务码: %v", err)
	}
}

func TestMinimaxEngineRejectsMissingAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}, "base_resp": map[string]any{"status_code": 0}})
	}))
	defer server.Close()

	engine, err := NewMinimaxEngine(MinimaxConfig{BaseURL: server.URL, GroupID: "g", APIKey: "k"})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}
	if _, err := engine.Synthesize(context.Background(), "你好"); err == nil {
		t.Error("响应缺少音频应返回错误")
	}
}

// edge 是免凭据的默认引擎，音色留空要有默认值，不能让人第一次启动就失败。
func TestEdgeEngineDefaultsVoice(t *testing.T) {
	engine, err := NewEdgeEngine(EdgeConfig{})
	if err != nil {
		t.Fatalf("空配置应当能构造: %v", err)
	}
	if engine.Name() != "edge_tts" {
		t.Fatalf("引擎名 = %s", engine.Name())
	}

	typed, ok := engine.(*edgeEngine)
	if !ok {
		t.Fatalf("引擎类型 = %T", engine)
	}
	if typed.cfg.Voice != defaultEdgeVoice {
		t.Fatalf("默认音色 = %q, want %q", typed.cfg.Voice, defaultEdgeVoice)
	}
	if typed.cfg.ReceiveTimeout != defaultEdgeTimeout {
		t.Fatalf("默认超时 = %d, want %d", typed.cfg.ReceiveTimeout, defaultEdgeTimeout)
	}
}

func TestEngineValidation(t *testing.T) {
	cases := []struct {
		name string
		run  func() error
	}{

		{"openai 缺 base_url", func() error {
			_, err := NewOpenAICompatibleEngine(OpenAICompatibleConfig{Model: "m", Voice: "v"})
			return err
		}},
		{"openai 缺 model", func() error {
			_, err := NewOpenAICompatibleEngine(OpenAICompatibleConfig{BaseURL: "http://x", Voice: "v"})
			return err
		}},
		{"openai 缺 voice", func() error {
			_, err := NewOpenAICompatibleEngine(OpenAICompatibleConfig{BaseURL: "http://x", Model: "m"})
			return err
		}},
		{"fish 缺 api_key", func() error { _, err := NewFishEngine(FishConfig{}); return err }},
		{"minimax 缺 group_id", func() error {
			_, err := NewMinimaxEngine(MinimaxConfig{APIKey: "k"})
			return err
		}},
		{"minimax 缺 api_key", func() error {
			_, err := NewMinimaxEngine(MinimaxConfig{GroupID: "g"})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Error("应返回校验错误")
			}
		})
	}
}

func TestEngineDefaults(t *testing.T) {
	engine, err := NewMinimaxEngine(MinimaxConfig{GroupID: "g", APIKey: "k"})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}
	if engine.Name() != "minimax_tts" {
		t.Errorf("引擎名不符合预期: %q", engine.Name())
	}

	fish, err := NewFishEngine(FishConfig{APIKey: "k"})
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}
	if fish.Name() != "fish_api_tts" {
		t.Errorf("引擎名不符合预期: %q", fish.Name())
	}
}

// capture 把收到的请求整理成可直接断言的结构。
func capture(r *http.Request) capturedRequest {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	return capturedRequest{
		path:   r.URL.Path,
		auth:   r.Header.Get("Authorization"),
		query:  r.URL.Query(),
		body:   body,
		header: r.Header,
	}
}
