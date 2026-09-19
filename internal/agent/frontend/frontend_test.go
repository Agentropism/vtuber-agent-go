package frontend

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/shared/emotion"
	"github.com/Agentropism/vtuber-agent-go/internal/tts"

	"github.com/coder/websocket"
)

func TestEncodeWAV(t *testing.T) {
	pcm := make([]byte, 2400) // 0.05 秒 24kHz 单声道 16bit
	for i := range pcm {
		pcm[i] = byte(i % 251)
	}

	wav := encodeWAV(pcm)

	if len(wav) != wavHeaderSize+len(pcm) {
		t.Fatalf("长度 = %d, want %d", len(wav), wavHeaderSize+len(pcm))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[12:16]) != "fmt " || string(wav[36:40]) != "data" {
		t.Fatalf("WAV 魔数不对: %q %q %q %q", wav[0:4], wav[8:12], wav[12:16], wav[36:40])
	}
	if got := binary.LittleEndian.Uint32(wav[4:8]); got != uint32(36+len(pcm)) {
		t.Fatalf("RIFF 长度 = %d", got)
	}
	// 编码必须是 1（PCM），否则浏览器与 Cubism 的解码器都会拒绝
	if got := binary.LittleEndian.Uint16(wav[20:22]); got != 1 {
		t.Fatalf("音频格式 = %d, want 1(PCM)", got)
	}
	if got := binary.LittleEndian.Uint16(wav[22:24]); got != uint16(tts.Channels) {
		t.Fatalf("声道数 = %d", got)
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != uint32(tts.SampleRate) {
		t.Fatalf("采样率 = %d, want %d", got, tts.SampleRate)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != uint16(tts.BytesPerSample*8) {
		t.Fatalf("位深 = %d", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(pcm)) {
		t.Fatalf("data 长度 = %d, want %d", got, len(pcm))
	}
}

func TestAudioDuration(t *testing.T) {
	// 1 秒：24000 帧 × 2 字节
	pcm := make([]byte, tts.SampleRate*tts.BytesPerSample)
	if got := audioDuration(pcm); got != time.Second {
		t.Fatalf("时长 = %s, want 1s", got)
	}
	if got := audioDuration(nil); got != 0 {
		t.Fatalf("空音频时长 = %s, want 0", got)
	}
}

// 表情映射必须保留 JSON 里的键顺序：原实现靠插入序决定同名标签取哪个。
func TestOrderedEmotionMapKeepsOrder(t *testing.T) {
	var m orderedEmotionMap
	raw := `{"joy":3,"anger":2,"neutral":0}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("解析: %v", err)
	}

	want := []emotion.Entry{{Name: "joy", Index: 3}, {Name: "anger", Index: 2}, {Name: "neutral", Index: 0}}
	if len(m) != len(want) {
		t.Fatalf("条目数 = %d, want %d", len(m), len(want))
	}
	for i, entry := range want {
		if m[i] != entry {
			t.Fatalf("第 %d 项 = %#v, want %#v（必须保持 JSON 顺序）", i, m[i], entry)
		}
	}
}

func TestScanModels(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"mao_pro", "shizuku"} {
		full := filepath.Join(dir, name, "runtime")
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("建目录: %v", err)
		}
		if err := os.WriteFile(filepath.Join(full, name+".model3.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatalf("写模型文件: %v", err)
		}
	}

	entries, err := scanModels(dir)
	if err != nil {
		t.Fatalf("扫描: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("模型数 = %d, want 2", len(entries))
	}
	if entries[0].Name != "mao_pro" {
		t.Fatalf("第一个模型 = %s（应按名字排序）", entries[0].Name)
	}
	if entries[0].URL != "/live2d-models/mao_pro/runtime/mao_pro.model3.json" {
		t.Fatalf("模型地址 = %s", entries[0].URL)
	}
}

// 端到端：浏览器连上来 → 收到 hello → 播报 → 回执 → Play 返回。
func TestSinkDeliversAndWaitsForPlayback(t *testing.T) {
	dir := t.TempDir()
	modelDir := filepath.Join(dir, "mao_pro", "runtime")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	model3 := `{"FileReferences":{"Expressions":[{"Name":"exp_01"},{"Name":"exp_02"}]}}`
	if err := os.WriteFile(filepath.Join(modelDir, "mao_pro.model3.json"), []byte(model3), 0o600); err != nil {
		t.Fatalf("写模型文件: %v", err)
	}

	front, err := New(Config{
		Character: Character{Name: "Mili"},
		ModelName: "mao_pro",
		ModelsDir: dir,
	})
	if err != nil {
		t.Fatalf("构造前端: %v", err)
	}

	server := httptest.NewServer(front.ClientWSHandler())
	defer server.Close()

	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("连接 /client-ws: %v", err)
	}
	defer conn.CloseNow()

	// hello
	_, helloRaw, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("读取 hello: %v", err)
	}
	var hello struct {
		Type string `json:"type"`
		Data struct {
			Character Character `json:"character"`
			Model     modelInfo `json:"model"`
		} `json:"data"`
	}
	if err := json.Unmarshal(helloRaw, &hello); err != nil {
		t.Fatalf("解析 hello: %v", err)
	}
	if hello.Type != "hello" || hello.Data.Character.Name != "Mili" {
		t.Fatalf("hello 内容不对: %s", helloRaw)
	}
	if len(hello.Data.Model.Expressions) != 2 {
		t.Fatalf("表达式清单 = %v, want 2 项", hello.Data.Model.Expressions)
	}

	// 播报：Play 会阻塞到前端回执
	pcm := make([]byte, 4800) // 0.1 秒
	done := make(chan error, 1)
	go func() {
		done <- front.Sink().Play(context.Background(), broadcast.Item{
			Priority: broadcast.PriorityDanmaku,
			Text:     "测试播报",
			Emotion:  "joy",
			Source:   "test",
		}, pcm)
	}()

	_, speakRaw, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("读取 speak: %v", err)
	}
	var speak struct {
		Type string    `json:"type"`
		Data speakData `json:"data"`
	}
	if err := json.Unmarshal(speakRaw, &speak); err != nil {
		t.Fatalf("解析 speak: %v", err)
	}
	if speak.Type != "speak" || speak.Data.Text != "测试播报" {
		t.Fatalf("speak 内容不对: %s", speakRaw)
	}
	// mao_pro 的 joy 是下标 3，这里用兜底词表，取到即算通过；未命中应为 -1
	if speak.Data.Emotion < -1 {
		t.Fatalf("表情下标异常: %d", speak.Data.Emotion)
	}
	if speak.Data.Duration != 100 {
		t.Fatalf("时长 =d %d ms, want 100", speak.Data.Duration)
	}

	audio, err := base64.StdEncoding.DecodeString(speak.Data.Audio)
	if err != nil {
		t.Fatalf("解码音频: %v", err)
	}
	if len(audio) != wavHeaderSize+len(pcm) || string(audio[0:4]) != "RIFF" {
		t.Fatalf("下发的音频不是完整 WAV: %d 字节", len(audio))
	}

	// 回执前 Play 不应返回
	select {
	case err := <-done:
		t.Fatalf("前端还没回执，Play 就返回了: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	reply, _ := json.Marshal(map[string]any{"type": "playback-finished", "data": map[string]any{"seq": speak.Data.Seq}})
	if err := conn.Write(context.Background(), websocket.MessageText, reply); err != nil {
		t.Fatalf("回执: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Play 返回错误: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("收到回执后 Play 仍未返回")
	}
}

// 没有前端连接时直接返回，不阻塞播报队列。
func TestSinkWithoutClients(t *testing.T) {
	front, err := New(Config{Character: Character{Name: "Mili"}, ModelsDir: t.TempDir()})
	if err == nil {
		t.Fatal("没有模型时应报错")
	}
	_ = front
}

func TestModelInfoEndpoint(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "mao_pro", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "mao_pro.model3.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("写模型文件: %v", err)
	}

	front, err := New(Config{
		Character: Character{Name: "Mili", Avatar: "mao.png"},
		ModelsDir: dir,
	})
	if err != nil {
		t.Fatalf("构造前端: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/live2d-models/info", nil)
	front.ModelsHandler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", recorder.Code)
	}
	var response modelInfoResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if response.Count != 1 || len(response.Characters) != 1 {
		t.Fatalf("清单内容不对: %s", recorder.Body.String())
	}
	if response.Characters[0].ModelPath != "/live2d-models/mao_pro/runtime/mao_pro.model3.json" {
		t.Fatalf("模型路径 = %s", response.Characters[0].ModelPath)
	}
}

// 内置页面必须真的能被访问到（嵌入资源漏了就白屏）。
func TestWebHandlerServesPage(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "mao_pro", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "mao_pro.model3.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("写模型文件: %v", err)
	}

	front, err := New(Config{ModelsDir: dir})
	if err != nil {
		t.Fatalf("构造前端: %v", err)
	}

	// "/web/" 由 FileServer 直接给 index.html（显式请求 /index.html 会被它跳到 ./）
	cases := []struct {
		requestPath string
		contains    string
	}{
		{"/web/", "<canvas"},
		{"/web/app.js", "client-ws"},
		{"/web/libs/pixi.min.js", "pixi.js"},
		{"/web/libs/live2dcubismcore.min.js", "Live2D"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		front.WebHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.requestPath, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d", tc.requestPath, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), tc.contains) {
			t.Fatalf("%s 内容里没有 %q（嵌入资源可能漏了）", tc.requestPath, tc.contains)
		}
	}
}
