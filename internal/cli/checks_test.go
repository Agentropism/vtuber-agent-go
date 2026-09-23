package cli

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
)

// validConfig 返回一份「开着就能跑」的最小配置，用例按需改其中的字段。
//
// 端口用 ":0"：让内核挑一个空闲端口，探测必然成功，也不会占用固定端口。
func validConfig() *config.Config {
	var cfg config.Config
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.LLM.BaseURL = "https://api.example.com/v1"
	cfg.LLM.Model = "test-model"
	cfg.LLM.APIKey = "sk-test"

	return &cfg
}

func writeTempFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

// writeModel 造一个最小可被扫到的 Live2D 模型目录：<dir>/<name>/runtime/<name>.model3.json。
func writeModel(t *testing.T, dir, name string) {
	t.Helper()
	writeTempFile(t, filepath.Join(dir, name, "runtime", name+".model3.json"), "{}")
}

func TestCheckConfigVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(cfg *config.Config)
		want    checkStatus
		message string
	}{
		{"最小可用配置", func(*config.Config) {}, statusPass, "解析通过"},
		{"addr 为空", func(cfg *config.Config) { cfg.Server.Addr = " " }, statusFail, "[server].addr 为空"},
		{"base_url 为空", func(cfg *config.Config) { cfg.LLM.BaseURL = "" }, statusFail, "[llm].base_url 为空"},
		{"model 为空", func(cfg *config.Config) { cfg.LLM.Model = "" }, statusFail, "[llm].model 为空"},
		{
			"开了推流却不知道往哪推",
			func(cfg *config.Config) { cfg.Stream.Enabled = true },
			statusFail, "既没有 output 也没有 room_id",
		},
		{
			"引擎名不认识",
			func(cfg *config.Config) { cfg.TTS.Engines = []string{"nope_tts"} },
			statusFail, "未知的 TTS 引擎",
		},
		{
			"引擎必填项不齐",
			func(cfg *config.Config) {
				cfg.TTS.Engines = []string{"openai_tts"}
				cfg.TTS.OpenAI.BaseURL = "https://api.example.com/v1"
				cfg.TTS.OpenAI.Model = "tts-1"
				// 故意不填 voice：真实构造函数要求它
			},
			statusFail, "缺少 voice",
		},
		{"免凭据引擎可用", func(cfg *config.Config) { cfg.TTS.Engines = []string{"edge_tts"} }, statusPass, "解析通过"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(cfg)

			got := checkConfig("config.toml", cfg, nil)
			if got.Name != "config" {
				t.Errorf("检查项名 = %q, want config", got.Name)
			}
			if got.Status != tc.want {
				t.Errorf("状态 = %q, want %q（消息: %s）", got.Status, tc.want, got.Message)
			}
			if !strings.Contains(got.Message, tc.message) {
				t.Errorf("消息 %q 不含 %q", got.Message, tc.message)
			}
		})
	}
}

// 配置读不出来时必须是 fail，并且把原始错误带出来（否则 doctor 只会说「失败」）。
func TestCheckConfigReportsLoadError(t *testing.T) {
	got := checkConfig("missing.toml", nil, os.ErrNotExist)
	if got.Status != statusFail {
		t.Errorf("状态 = %q, want fail", got.Status)
	}
	if !strings.Contains(got.Message, "file does not exist") {
		t.Errorf("消息应带出原始错误，实际 %q", got.Message)
	}
}

func TestCheckCharacter(t *testing.T) {
	t.Run("未配置时跳过", func(t *testing.T) {
		got := checkCharacter(validConfig())
		if got.Status != statusSkip {
			t.Errorf("状态 = %q, want skip", got.Status)
		}
	})

	t.Run("文件缺失是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.Agent.CharacterFile = filepath.Join(t.TempDir(), "nope.toml")

		got := checkCharacter(cfg)
		if got.Status != statusFail {
			t.Errorf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
	})

	t.Run("合法角色文件通过", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mili.toml")
		writeTempFile(t, path, "name = \"Mili\"\nlive2d_model = \"mao_pro\"\n")

		cfg := validConfig()
		cfg.Agent.CharacterFile = path

		got := checkCharacter(cfg)
		if got.Status != statusPass {
			t.Fatalf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
		if !strings.Contains(got.Message, "Mili") || !strings.Contains(got.Message, "mao_pro") {
			t.Errorf("消息应带上角色与模型，实际 %q", got.Message)
		}
	})

	t.Run("没写角色名是警告", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mili.toml")
		writeTempFile(t, path, "live2d_model = \"mao_pro\"\n")

		cfg := validConfig()
		cfg.Agent.CharacterFile = path

		if got := checkCharacter(cfg); got.Status != statusWarn {
			t.Errorf("状态 = %q, want warn", got.Status)
		}
	})
}

func TestCheckPaths(t *testing.T) {
	t.Run("未配置时跳过", func(t *testing.T) {
		if got := checkPaths(validConfig()); got.Status != statusSkip {
			t.Errorf("状态 = %q, want skip", got.Status)
		}
	})

	t.Run("新路径的父目录可写即通过", func(t *testing.T) {
		cfg := validConfig()
		cfg.Agent.MemoryFile = filepath.Join(t.TempDir(), "data", "memory.jsonl")

		got := checkPaths(cfg)
		if got.Status != statusPass {
			t.Errorf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
	})

	// 用「上级是文件」当失败样本：不依赖运行用户是不是 root，权限位判定不会翻车。
	t.Run("上级是文件是失败", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "blocker")
		writeTempFile(t, blocker, "not a dir")

		cfg := validConfig()
		cfg.Agent.MemoryFile = filepath.Join(blocker, "data", "memory.jsonl")

		got := checkPaths(cfg)
		if got.Status != statusFail {
			t.Errorf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
		if !strings.Contains(got.Message, "不是目录") {
			t.Errorf("消息应说明上级不是目录，实际 %q", got.Message)
		}
	})

	t.Run("既有记忆文件坏行是失败", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.jsonl")
		writeTempFile(t, path, "{\"ok\":1}\n这不是 JSON\n")

		cfg := validConfig()
		cfg.Agent.MemoryFile = path

		got := checkPaths(cfg)
		if got.Status != statusFail {
			t.Fatalf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
		if !strings.Contains(got.Message, "第 2 行") {
			t.Errorf("消息应指出坏行行号，实际 %q", got.Message)
		}
	})

	t.Run("合法 JSONL 通过", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.jsonl")
		writeTempFile(t, path, "{\"ok\":1}\n\n{\"ok\":2}\n")

		cfg := validConfig()
		cfg.Agent.MemoryFile = path

		if got := checkPaths(cfg); got.Status != statusPass {
			t.Errorf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
	})
}

func TestCheckPort(t *testing.T) {
	t.Run("未配置时跳过", func(t *testing.T) {
		cfg := validConfig()
		cfg.Server.Addr = ""

		if got := checkPort(cfg); got.Status != statusSkip {
			t.Errorf("状态 = %q, want skip", got.Status)
		}
	})

	t.Run("空闲端口通过", func(t *testing.T) {
		if got := checkPort(validConfig()); got.Status != statusPass {
			t.Errorf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
	})

	t.Run("占用端口失败", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("占端口失败: %v", err)
		}
		defer listener.Close()

		cfg := validConfig()
		cfg.Server.Addr = listener.Addr().String()

		got := checkPort(cfg)
		if got.Status != statusFail {
			t.Errorf("状态 = %q, want fail", got.Status)
		}
		if got.Details["addr"] != cfg.Server.Addr {
			t.Errorf("details.addr = %v, want %s", got.Details["addr"], cfg.Server.Addr)
		}
	})
}

func TestCheckBinaries(t *testing.T) {
	t.Run("未启用推流时跳过", func(t *testing.T) {
		if got := checkBinaries(validConfig()); got.Status != statusSkip {
			t.Errorf("状态 = %q, want skip", got.Status)
		}
	})

	t.Run("缺 ffmpeg 是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.Stream.Enabled = true
		cfg.Stream.FFmpeg = "definitely-not-a-real-binary-xyz"

		got := checkBinaries(cfg)
		if got.Status != statusFail {
			t.Errorf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
		if !strings.Contains(got.Message, "definitely-not-a-real-binary-xyz") {
			t.Errorf("消息应指出缺哪个可执行文件，实际 %q", got.Message)
		}
	})

	t.Run("input=test 不需要 Xvfb 与 Chrome", func(t *testing.T) {
		cfg := validConfig()
		cfg.Stream.Enabled = true
		cfg.Stream.Renderer = true
		cfg.Stream.Input = "test"
		cfg.Stream.FFmpeg = "true" // 一定存在的可执行文件
		cfg.Stream.Xvfb = "definitely-not-a-real-xvfb-xyz"
		cfg.Stream.Chrome = "definitely-not-a-real-chrome-xyz"

		if got := checkBinaries(cfg); got.Status != statusPass {
			t.Errorf("input=test 时不该要求 Xvfb/Chrome，状态 = %q（%s）", got.Status, got.Message)
		}
	})

	t.Run("screen 渲染缺 Xvfb 是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.Stream.Enabled = true
		cfg.Stream.Renderer = true
		cfg.Stream.Input = "screen"
		cfg.Stream.FFmpeg = "true"
		cfg.Stream.Xvfb = "definitely-not-a-real-xvfb-xyz"
		cfg.Stream.Chrome = "true"

		got := checkBinaries(cfg)
		if got.Status != statusFail {
			t.Errorf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
	})

	t.Run("renderer=false 时不需要显示与浏览器", func(t *testing.T) {
		cfg := validConfig()
		cfg.Stream.Enabled = true
		cfg.Stream.Input = "screen"
		cfg.Stream.FFmpeg = "true"
		cfg.Stream.Xvfb = "definitely-not-a-real-xvfb-xyz"
		cfg.Stream.Chrome = "definitely-not-a-real-chrome-xyz"

		if got := checkBinaries(cfg); got.Status != statusPass {
			t.Errorf("自备显示时不该要求 Xvfb/Chrome，状态 = %q（%s）", got.Status, got.Message)
		}
	})
}

func TestCheckCredentials(t *testing.T) {
	t.Run("缺 LLM key 是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.LLM.APIKey = ""

		got := checkCredentials(cfg)
		if got.Status != statusFail {
			t.Fatalf("状态 = %q, want fail", got.Status)
		}
		if !strings.Contains(got.Message, "LLM_API_KEY") {
			t.Errorf("消息应给出环境变量替代写法，实际 %q", got.Message)
		}
	})

	t.Run("免凭据引擎不需要 key", func(t *testing.T) {
		cfg := validConfig()
		cfg.TTS.Engines = []string{"edge_tts"}

		if got := checkCredentials(cfg); got.Status != statusPass {
			t.Errorf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
	})

	t.Run("启用引擎缺 key 是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.TTS.Engines = []string{"openai_tts"}

		got := checkCredentials(cfg)
		if got.Status != statusFail {
			t.Fatalf("状态 = %q, want fail", got.Status)
		}
		if !strings.Contains(got.Message, "OPENAI_API_KEY") {
			t.Errorf("消息应指出环境变量名，实际 %q", got.Message)
		}
	})

	// 登录态可以事后扫码补（app.provideStream 允许先启动），所以是警告：开播前补上即可。
	t.Run("缺 B 站登录态是警告", func(t *testing.T) {
		cfg := validConfig()
		cfg.Stream.Enabled = true
		cfg.Stream.RoomID = 123456

		got := checkCredentials(cfg)
		if got.Status != statusWarn {
			t.Fatalf("状态 = %q, want warn（%s）", got.Status, got.Message)
		}
		if !strings.Contains(got.Message, "/login/") {
			t.Errorf("消息应指出可以扫码补，实际 %q", got.Message)
		}
	})

	t.Run("手填 output 时不要求登录态", func(t *testing.T) {
		cfg := validConfig()
		cfg.Stream.Enabled = true
		cfg.Stream.Output = "rtmp://example.com/live/key"

		if got := checkCredentials(cfg); got.Status != statusPass {
			t.Errorf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
	})

	t.Run("失败时同时带出警告", func(t *testing.T) {
		cfg := validConfig()
		cfg.LLM.APIKey = ""
		cfg.Stream.Enabled = true
		cfg.Stream.RoomID = 123456

		got := checkCredentials(cfg)
		if got.Status != statusFail {
			t.Fatalf("状态 = %q, want fail", got.Status)
		}
		if !strings.Contains(got.Message, "BILIBILI_COOKIE") {
			t.Errorf("失败消息里不该丢掉登录态警告，实际 %q", got.Message)
		}
	})
}

func TestCheckFrontend(t *testing.T) {
	t.Run("未配置时跳过", func(t *testing.T) {
		if got := checkFrontend(validConfig(), config.Character{}); got.Status != statusSkip {
			t.Errorf("状态 = %q, want skip", got.Status)
		}
	})

	t.Run("启用但没给模型目录是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.Frontend.Enabled = true

		if got := checkFrontend(cfg, config.Character{}); got.Status != statusFail {
			t.Errorf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
	})

	t.Run("模型目录不存在是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.Frontend.ModelsDir = filepath.Join(t.TempDir(), "nope")

		if got := checkFrontend(cfg, config.Character{}); got.Status != statusFail {
			t.Errorf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
	})

	t.Run("目录里没有模型是失败", func(t *testing.T) {
		cfg := validConfig()
		cfg.Frontend.ModelsDir = t.TempDir()

		got := checkFrontend(cfg, config.Character{})
		if got.Status != statusFail {
			t.Fatalf("状态 = %q, want fail（%s）", got.Status, got.Message)
		}
		if !strings.Contains(got.Message, "model3.json") {
			t.Errorf("消息应说明缺什么文件，实际 %q", got.Message)
		}
	})

	t.Run("扫到模型即通过", func(t *testing.T) {
		dir := t.TempDir()
		writeModel(t, dir, "mao_pro")

		cfg := validConfig()
		cfg.Frontend.ModelsDir = dir

		got := checkFrontend(cfg, config.Character{Live2DModel: "mao_pro"})
		if got.Status != statusPass {
			t.Fatalf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
		models, ok := got.Details["models"].([]string)
		if !ok || len(models) != 1 || models[0] != "mao_pro" {
			t.Errorf("details.models = %#v, want [mao_pro]", got.Details["models"])
		}
	})

	// 运行时会退回第一个模型（web.pickModel），所以这是警告而不是失败。
	t.Run("角色模型不在清单里是警告", func(t *testing.T) {
		dir := t.TempDir()
		writeModel(t, dir, "other_model")

		cfg := validConfig()
		cfg.Frontend.ModelsDir = dir

		got := checkFrontend(cfg, config.Character{Live2DModel: "mao_pro"})
		if got.Status != statusWarn {
			t.Fatalf("状态 = %q, want warn（%s）", got.Status, got.Message)
		}
		if !strings.Contains(got.Message, "other_model") {
			t.Errorf("消息应指出会退到哪个模型，实际 %q", got.Message)
		}
	})

	t.Run("清单文件优先于目录扫描", func(t *testing.T) {
		dir := t.TempDir()
		writeModel(t, dir, "scanned_model")

		dict := filepath.Join(t.TempDir(), "model_dict.json")
		writeTempFile(t, dict, `[{"name":"from_dict","url":"/live2d-models/from_dict/runtime/x.model3.json"}]`)

		cfg := validConfig()
		cfg.Frontend.ModelsDir = dir
		cfg.Frontend.ModelDict = dict

		got := checkFrontend(cfg, config.Character{Live2DModel: "from_dict"})
		if got.Status != statusPass {
			t.Fatalf("状态 = %q, want pass（%s）", got.Status, got.Message)
		}
		models := got.Details["models"].([]string)
		if len(models) != 1 || models[0] != "from_dict" {
			t.Errorf("清单应优先：models = %#v", models)
		}
	})
}

// json.Fields 只为把「details 能被编码」这件事钉住：doctor 的 --json 会把它一起写出去。
func TestCheckDetailsAreJSONEncodable(t *testing.T) {
	got := checkResult{Name: "x", Status: statusPass, Message: "m", Details: map[string]any{"models": []string{"a"}}}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if !strings.Contains(string(raw), `"details"`) {
		t.Errorf("details 应出现在 JSON 里: %s", raw)
	}
}
