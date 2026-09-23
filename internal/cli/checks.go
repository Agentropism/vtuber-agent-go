package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/app"
	"github.com/Agentropism/vtuber-agent-go/internal/backend/web"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/stream"
)

// doctorCheckNames 是 doctor 的检查项顺序，与 docs/CLI.md 的表格一一对应。
// 契约的一部分：只允许往末尾追加，改名算破坏性变更。
var doctorCheckNames = []string{"config", "character", "paths", "port", "binaries", "credentials", "frontend"}

// wOK 是 access(2) 的「可写」掩码。Linux 上取值固定为 2，而 syscall 没有稳定导出这个名字。
const wOK = 2

// runDoctor 依次跑完全部检查。
//
// 配置读不出来时其余检查没有可用的输入，一律标 skip：doctor 的价值是给出准确的
// 第一层原因，而不是拿零值凑一堆二次错误出来。
func runDoctor(configPath string) doctorReport {
	cfg, loadErr := config.ProvideConfigFrom(configPath)
	checks := []checkResult{checkConfig(configPath, cfg, loadErr)}

	if cfg == nil {
		for _, name := range doctorCheckNames[1:] {
			checks = append(checks, newCheck(name, statusSkip, "配置不可用，跳过"))
		}
	} else {
		character, _ := config.LoadCharacter(cfg.Agent.CharacterFile)
		checks = append(checks,
			checkCharacter(cfg),
			checkPaths(cfg),
			checkPort(cfg),
			checkBinaries(cfg),
			checkCredentials(cfg),
			checkFrontend(cfg, character),
		)
	}

	counts := summarize(checks)

	return doctorReport{
		OK:      !hasFailure(checks),
		Config:  configPath,
		Checks:  checks,
		Summary: counts,
	}
}

// checkConfig 检查配置能否解析、必填项与取值是否说得通。
func checkConfig(configPath string, cfg *config.Config, loadErr error) checkResult {
	const name = "config"
	if loadErr != nil {
		return newCheck(name, statusFail, loadErr.Error())
	}

	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if strings.TrimSpace(cfg.Server.Addr) == "" {
		add("[server].addr 为空")
	}
	if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		add("[llm].base_url 为空")
	}
	if strings.TrimSpace(cfg.LLM.Model) == "" {
		add("[llm].model 为空")
	}
	// 与 app.provideStream 的判据一致：开了推流却不知道往哪推，是装配期就会失败的那种配置。
	if cfg.Stream.Enabled && strings.TrimSpace(cfg.Stream.Output) == "" && cfg.Stream.RoomID == 0 {
		add("[stream] 已启用，但既没有 output 也没有 room_id：不知道往哪推")
	}
	// 引擎名与必填项用真实的构造函数判定，避免这里另立一套规则。
	if err := app.CheckTTSEngines(cfg); err != nil {
		add("%v", err)
	}

	if len(problems) > 0 {
		return newCheck(name, statusFail, strings.Join(problems, "；"))
	}

	return newCheck(name, statusPass, fmt.Sprintf("%s 解析通过", configPath))
}

// checkCharacter 检查角色资产能不能读出来。
func checkCharacter(cfg *config.Config) checkResult {
	const name = "character"

	path := strings.TrimSpace(cfg.Agent.CharacterFile)
	if path == "" {
		return newCheck(name, statusSkip, "未配置 [agent].character_file，使用内置兜底提示词")
	}

	character, err := config.LoadCharacter(path)
	if err != nil {
		return newCheck(name, statusFail, err.Error())
	}
	if strings.TrimSpace(character.Name) == "" {
		return newCheck(name, statusWarn, fmt.Sprintf("%s 里没有写角色名（name）", path))
	}

	message := fmt.Sprintf("%s 解析通过：角色 %s", path, character.Name)
	if character.Live2DModel != "" {
		message += fmt.Sprintf("，Live2D 模型 %s", character.Live2DModel)
	}

	return newCheck(name, statusPass, message)
}

// checkPaths 检查落盘路径可写、既有数据没坏。
//
// 只做只读校验：不建目录、不落文件，可写性用 access(2) 问内核。
func checkPaths(cfg *config.Config) checkResult {
	const name = "paths"

	var problems, checked []string
	memoryFile := strings.TrimSpace(cfg.Agent.MemoryFile)
	archiveDir := strings.TrimSpace(cfg.Agent.ArchiveDir)

	if memoryFile != "" {
		checked = append(checked, "[agent].memory_file")
		if err := checkWritable(memoryFile); err != nil {
			problems = append(problems, fmt.Sprintf("[agent].memory_file=%s %v", memoryFile, err))
		} else if err := checkJSONLines(memoryFile); err != nil {
			// 路径本身没问题才去看内容：否则同一个原因会报两遍
			problems = append(problems, err.Error())
		}
	}

	if archiveDir != "" {
		checked = append(checked, "[agent].archive_dir")
		if err := checkWritable(archiveDir); err != nil {
			problems = append(problems, fmt.Sprintf("[agent].archive_dir=%s %v", archiveDir, err))
		}
	}

	if len(problems) > 0 {
		return newCheck(name, statusFail, strings.Join(problems, "；"))
	}
	if len(checked) == 0 {
		return newCheck(name, statusSkip, "未配置 memory_file / archive_dir，跳过落盘检查")
	}

	return newCheck(name, statusPass, fmt.Sprintf("%s 可写", strings.Join(checked, "、")))
}

// checkWritable 判断路径能不能写。
//
// 路径可能还不存在（首次运行），所以往上找最近一个已存在的祖先：目录看它可不可写，
// 中途撞上文件说明这个路径永远建不出来。
func checkWritable(path string) error {
	probe := path
	for {
		info, err := os.Stat(probe)
		if err == nil {
			if probe != path && !info.IsDir() {
				return fmt.Errorf("的上级 %s 不是目录", probe)
			}
			break
		}
		// 权限问题就地报，别再往上找：真正的障碍就在这里。
		if os.IsPermission(err) {
			return fmt.Errorf("不可访问: %w", err)
		}

		parent := filepath.Dir(probe)
		if parent == probe {
			return fmt.Errorf("的上级目录都不存在")
		}
		probe = parent
	}

	if err := syscall.Access(probe, wOK); err != nil {
		return fmt.Errorf("不可写（%s）", probe)
	}

	return nil
}

// checkJSONLines 逐行校验 JSON Lines 文件；文件不存在不算问题（还没写过）。
func checkJSONLines(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s 打不开: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// 单行可能很长（一条记忆里带完整回复），默认 64KB 会误报「行太长」。
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		if !json.Valid([]byte(raw)) {
			return fmt.Errorf("%s 第 %d 行不是合法 JSON", path, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s 读取失败: %w", path, err)
	}

	return nil
}

// checkPort 用 bind 探测端口能否拿来用。
//
// 扫 /proc/net/tcp 会看错 TIME_WAIT、SO_REUSEADDR、只听 IPv6 这些情况，而「端口的
// 可用性」正是启动时真正会碰到的错误。绑上就立刻释放，不落任何东西。
func checkPort(cfg *config.Config) checkResult {
	const name = "port"

	addr := strings.TrimSpace(cfg.Server.Addr)
	if addr == "" {
		return newCheck(name, statusSkip, "未配置 [server].addr")
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		result := newCheck(name, statusFail, fmt.Sprintf("%s 不可用: %v", addr, err))
		result.Details = map[string]any{"addr": addr}

		return result
	}
	bound := listener.Addr().String()
	_ = listener.Close()

	return newCheck(name, statusPass, fmt.Sprintf("%s 可绑定（%s）", addr, bound))
}

// checkBinaries 检查推流需要的外部命令，判据完全由配置推导。
//
// 只有 [stream].enabled 才需要 ffmpeg；只有真的用渲染器抓虚拟屏才需要 Xvfb 与 Chrome
// （input = "test" 用 ffmpeg 自带画面，不需要显示与浏览器）。node / python3 只是构建期
// 依赖，不在这里报——那会把「没装 node」变成运行环境的假告警。
func checkBinaries(cfg *config.Config) checkResult {
	const name = "binaries"
	if !cfg.Stream.Enabled {
		return newCheck(name, statusSkip, "未启用 [stream]，不需要 ffmpeg / Xvfb / Chrome")
	}

	needed := []struct{ field, binary string }{
		{"[stream].ffmpeg", orDefault(cfg.Stream.FFmpeg, stream.DefaultFFmpeg)},
	}
	if cfg.Stream.Renderer && cfg.Stream.Input != string(stream.InputTest) {
		needed = append(needed,
			struct{ field, binary string }{"[stream].xvfb", orDefault(cfg.Stream.Xvfb, stream.DefaultXvfb)},
			struct{ field, binary string }{"[stream].chrome", orDefault(cfg.Stream.Chrome, stream.DefaultChrome)},
		)
	}

	var missing []string
	found := make([]string, 0, len(needed))
	for _, item := range needed {
		if _, err := exec.LookPath(item.binary); err != nil {
			missing = append(missing, fmt.Sprintf("%s（%s）", item.binary, item.field))
			continue
		}
		found = append(found, item.binary)
	}

	if len(missing) > 0 {
		return newCheck(name, statusFail, "PATH 里找不到 "+strings.Join(missing, "、"))
	}

	return newCheck(name, statusPass, "已找到 "+strings.Join(found, "、"))
}

// checkCredentials 只判「配了没有」：凭据有效性要联网才知道，而 doctor 是离线自检。
func checkCredentials(cfg *config.Config) checkResult {
	const name = "credentials"

	var missing, warnings []string
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		missing = append(missing, "[llm].api_key（或环境变量 LLM_API_KEY）")
	}

	for _, engine := range cfg.TTS.Engines {
		slot := ttsCredentialSlot(cfg, engine)
		if !slot.known || !slot.needs {
			continue
		}
		if strings.TrimSpace(slot.value) == "" {
			missing = append(missing, fmt.Sprintf("%s 的 api_key（或环境变量 %s）", engine, slot.envName))
		}
	}

	// 手填 output 时不需要登录态；否则开播接口要有 Cookie——但扫码登录页可以事后补上
	// （app.provideStream 允许先启动，见 waitForCookie），所以这里是警告而不是失败。
	if cfg.Stream.Enabled && strings.TrimSpace(cfg.Stream.Output) == "" && strings.TrimSpace(cfg.Stream.Cookie) == "" {
		warnings = append(warnings, "[stream].cookie（或环境变量 BILIBILI_COOKIE）：开播前在 /login/ 扫码也能补")
	}

	switch {
	case len(missing) > 0:
		// 失败时把警告一并带出去，别把「还差什么」藏起来
		return newCheck(name, statusFail, "缺少 "+strings.Join(append(missing, warnings...), "、"))
	case len(warnings) > 0:
		return newCheck(name, statusWarn, "缺少 "+strings.Join(warnings, "、"))
	}

	return newCheck(name, statusPass, "已配的凭据都就位（未验证有效性）")
}

// credentialSlot 描述一个引擎的凭据槽位。
type credentialSlot struct {
	value   string // 生效值（环境变量已由 config.applyEnv 补齐）
	envName string // 对应的环境变量名，报错时告诉用户替代写法
	needs   bool   // 该引擎是否需要凭据（edge_tts 免凭据）
	known   bool   // 引擎名是否认识（不认识的由 config 检查报，这里不重复）
}

// ttsCredentialSlot 返回某个 TTS 引擎的凭据槽位。
func ttsCredentialSlot(cfg *config.Config, engine string) credentialSlot {
	switch engine {
	case "edge_tts":
		return credentialSlot{needs: false, known: true}
	case "openai_tts":
		return credentialSlot{value: cfg.TTS.OpenAI.APIKey, envName: "OPENAI_API_KEY", needs: true, known: true}
	case "siliconflow_tts":
		return credentialSlot{value: cfg.TTS.SiliconFlow.APIKey, envName: "SILICONFLOW_API_KEY", needs: true, known: true}
	case "fish_api_tts":
		return credentialSlot{value: cfg.TTS.Fish.APIKey, envName: "FISH_API_KEY", needs: true, known: true}
	case "minimax_tts":
		return credentialSlot{value: cfg.TTS.Minimax.APIKey, envName: "MINIMAX_API_KEY", needs: true, known: true}
	default:
		return credentialSlot{}
	}
}

// checkFrontend 检查前端模型资产。
//
// 与 web 包共用 AvailableModels：模型名从哪来（清单优先、目录兜底）只有一个实现，
// 这份检查只读，不装配前端、也不往日志里写东西。
func checkFrontend(cfg *config.Config, character config.Character) checkResult {
	const name = "frontend"

	front := cfg.Frontend
	if !front.Enabled && strings.TrimSpace(front.ModelsDir) == "" {
		return newCheck(name, statusSkip, "未配置 [frontend]，跳过前端接入")
	}
	if strings.TrimSpace(front.ModelsDir) == "" {
		return newCheck(name, statusFail, "[frontend] 已启用，但 models_dir 为空")
	}

	info, err := os.Stat(front.ModelsDir)
	if err != nil {
		return newCheck(name, statusFail, fmt.Sprintf("模型目录不可用: %v", err))
	}
	if !info.IsDir() {
		return newCheck(name, statusFail, fmt.Sprintf("%s 不是目录", front.ModelsDir))
	}

	models, err := web.AvailableModels(front.ModelsDir, front.ModelDict)
	if err != nil {
		return newCheck(name, statusFail, fmt.Sprintf("列举模型失败: %v", err))
	}
	if len(models) == 0 {
		return newCheck(name, statusFail,
			fmt.Sprintf("在 %s 下没有找到任何 <模型名>/runtime/*.model3.json", front.ModelsDir))
	}

	if want := strings.TrimSpace(character.Live2DModel); want != "" && !slices.Contains(models, want) {
		result := newCheck(name, statusWarn,
			fmt.Sprintf("角色指定的模型 %q 不在清单里（运行时会退到 %q）", want, models[0]))
		result.Details = map[string]any{"models": models}

		return result
	}

	result := newCheck(name, statusPass, fmt.Sprintf("可用模型 %d 个", len(models)))
	result.Details = map[string]any{"models": models}

	return result
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}
