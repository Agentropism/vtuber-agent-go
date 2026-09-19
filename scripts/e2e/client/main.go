// 端到端冒烟的客户端：扮演前端与平台客户端，断言四条链路。
//
// 覆盖的场景：
//
//	第 1 轮  普通弹幕       → 流式回复切句 → 2 句播报
//	第 2 轮  触发工具的弹幕 → memory_search 工具调用 → 最终答复 → 1 句播报
//	第 3 轮  POST /inject   → 注入播报（带表情）
//	第 4 轮  静默           → 主动发言，最低优先级
//
// 每收到一条播报都校验音频是合法的 24kHz 单声道 PCM WAV，并回执
// playback-finished——队列要等回执才继续，不回执后面全都会卡住。
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"
)

const (
	// speakTimeout 是等待一条播报的上限。主动发言那一轮要等静默计时到点，
	// 所以比其它轮宽松。
	speakTimeout = 30 * time.Second

	normalDanmaku  = "主播今天玩什么？[joy]"
	toolDanmaku    = "你还记得我们聊过什么吗"
	toolReplyText  = "我记得你们刚聊过游戏。"
	injectText     = "这是注入的播报"
	idlePriority   = "idle"
	injectSource   = "inject"
	normalExpected = 2
	joyExpression  = 3 // mao_pro 的 emotionMap 里 joy 对应 exp_04
)

func main() {
	addr := os.Getenv("SMOKE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:16199"
	}

	if err := run(addr); err != nil {
		fmt.Fprintln(os.Stderr, "端到端冒烟失败:", err)
		os.Exit(1)
	}
	fmt.Println("端到端冒烟通过")
}

func run(addr string) error {
	if !waitPort(addr, 10*time.Second) {
		return fmt.Errorf("网关 %s 没有起来", addr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*speakTimeout)
	defer cancel()

	front, err := dial(ctx, "ws://"+addr+"/client-ws")
	if err != nil {
		return fmt.Errorf("连接前端通道: %w", err)
	}
	defer front.CloseNow()

	hello, err := readMessage(ctx, front)
	if err != nil {
		return fmt.Errorf("读取 hello: %w", err)
	}
	if hello.Type != "hello" {
		return fmt.Errorf("第一条消息应当是 hello，实际是 %s", hello.Type)
	}
	fmt.Printf("前端收到 hello：角色=%s 模型=%s\n", hello.Data.Character.Name, hello.Data.Model.Name)

	bili, err := dial(ctx, "ws://"+addr+"/bilibili")
	if err != nil {
		return fmt.Errorf("连接 B 站通道: %w", err)
	}
	defer bili.CloseNow()

	// 第 1 轮：普通弹幕，回复被切成两句
	if err := bili.Write(ctx, websocket.MessageText, danmaku(normalDanmaku, "dm-smoke-1")); err != nil {
		return fmt.Errorf("上报弹幕: %w", err)
	}
	for i := 0; i < normalExpected; i++ {
		if _, err := awaitSpeak(ctx, front, fmt.Sprintf("第 %d 句普通回复", i+1), nil); err != nil {
			return err
		}
	}
	fmt.Println("第 1 轮通过：普通弹幕 → 逐句播报")

	// 第 2 轮：触发 memory_search 的弹幕
	if err := bili.Write(ctx, websocket.MessageText, danmaku(toolDanmaku, "dm-smoke-2")); err != nil {
		return fmt.Errorf("上报弹幕: %w", err)
	}
	if _, err := awaitSpeak(ctx, front, "工具调用后的答复", func(speak speakData) bool {
		return speak.Text == toolReplyText
	}); err != nil {
		return err
	}
	fmt.Println("第 2 轮通过：工具调用 → 回灌结果 → 最终答复")

	// 第 3 轮：外部注入播报
	if err := inject(addr, injectText, "joy"); err != nil {
		return err
	}
	injected, err := awaitSpeak(ctx, front, "注入播报", func(speak speakData) bool {
		return speak.Source == injectSource
	})
	if err != nil {
		return err
	}
	if injected.Emotion != joyExpression {
		return fmt.Errorf("注入播报的表情下标 = %d, want %d（mao_pro 的 joy）", injected.Emotion, joyExpression)
	}
	fmt.Println("第 3 轮通过：/inject → 注入播报（带表情）")

	// 第 4 轮：静默一会儿，等主动发言（最低优先级）
	idle, err := awaitSpeak(ctx, front, "主动发言", func(speak speakData) bool {
		return speak.Priority == idlePriority
	})
	if err != nil {
		return err
	}
	fmt.Printf("第 4 轮通过：主动发言（priority=%s）：%s\n", idle.Priority, idle.Text)

	return nil
}

// downMessage 是下行消息信封。
type downMessage struct {
	Type string    `json:"type"`
	Data speakData `json:"data"`
}

// speakData 是播报消息的内容。
type speakData struct {
	Seq      uint64 `json:"seq"`
	Text     string `json:"text"`
	Emotion  int    `json:"emotion"`
	Audio    string `json:"audio"`
	Priority string `json:"priority"`
	Source   string `json:"source"`
	Duration int    `json:"duration_ms"`

	Character struct {
		Name string `json:"name"`
	} `json:"character"`
	Model struct {
		Name string `json:"name"`
	} `json:"model"`
}

// awaitSpeak 一直读到匹配的播报为止；途中每条播报都校验并回执，避免队列卡住。
func awaitSpeak(
	ctx context.Context,
	conn *websocket.Conn,
	what string,
	match func(speakData) bool,
) (speakData, error) {
	deadline := time.Now().Add(speakTimeout)

	for time.Now().Before(deadline) {
		msg, err := readMessage(ctx, conn)
		if err != nil {
			return speakData{}, fmt.Errorf("等待%s: %w", what, err)
		}
		if msg.Type != "speak" {
			continue
		}
		if err := checkAudio(msg.Data); err != nil {
			return speakData{}, err
		}

		// 先回执再判断：不回执的话队列会一直等这一句
		reply, _ := json.Marshal(map[string]any{
			"type": "playback-finished",
			"data": map[string]any{"seq": msg.Data.Seq},
		})
		if err := conn.Write(ctx, websocket.MessageText, reply); err != nil {
			return speakData{}, fmt.Errorf("回执播报完成: %w", err)
		}

		fmt.Printf("  播报 #%d：priority=%s source=%s emotion=%d text=%q\n",
			msg.Data.Seq, msg.Data.Priority, msg.Data.Source, msg.Data.Emotion, msg.Data.Text)

		if match == nil || match(msg.Data) {
			return msg.Data, nil
		}
	}

	return speakData{}, fmt.Errorf("等待%s超时", what)
}

func readMessage(ctx context.Context, conn *websocket.Conn) (downMessage, error) {
	var msg downMessage

	readCtx, cancel := context.WithTimeout(ctx, speakTimeout)
	defer cancel()

	_, data, err := conn.Read(readCtx)
	if err != nil {
		return msg, err
	}
	err = json.Unmarshal(data, &msg)

	return msg, err
}

// checkAudio 校验播报音频：必须是 24kHz 单声道 16bit 的 PCM WAV。
func checkAudio(speak speakData) error {
	if speak.Text == "" {
		return fmt.Errorf("播报 #%d 文本为空", speak.Seq)
	}

	wav, err := base64.StdEncoding.DecodeString(speak.Audio)
	if err != nil {
		return fmt.Errorf("解码音频: %w", err)
	}
	if len(wav) <= 44 {
		return fmt.Errorf("音频太短: %d 字节", len(wav))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return fmt.Errorf("音频不是 WAV 容器")
	}
	if format := binary.LittleEndian.Uint16(wav[20:22]); format != 1 {
		return fmt.Errorf("音频编码 = %d, want 1(PCM)", format)
	}
	if channels := binary.LittleEndian.Uint16(wav[22:24]); channels != 1 {
		return fmt.Errorf("声道数 = %d, want 1", channels)
	}
	if rate := binary.LittleEndian.Uint32(wav[24:28]); rate != 24000 {
		return fmt.Errorf("采样率 = %d, want 24000", rate)
	}

	return nil
}

// inject 走一次 HTTP 注入。
func inject(addr, text, emotion string) error {
	body, _ := json.Marshal(map[string]any{"text": text, "emotion": emotion})

	resp, err := http.Post("http://"+addr+"/inject", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("调用 /inject: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/inject 返回 %d", resp.StatusCode)
	}

	return nil
}

// danmaku 造一条 B 站开放平台弹幕事件。
func danmaku(text, messageID string) []byte {
	payload := map[string]any{
		"cmd": "LIVE_OPEN_PLATFORM_DM",
		"data": map[string]any{
			"uname": "观众甲", "uid": 0, "open_id": "open_1", "union_id": "",
			"uface": "", "timestamp": time.Now().Unix(), "room_id": 123456,
			"msg": text, "msg_id": messageID, "guard_level": 0,
			"fans_medal_wearing_status": false, "fans_medal_name": "", "fans_medal_level": 0,
			"emoji_img_url": "", "dm_type": 0, "glory_level": 0,
			"reply_open_id": "", "reply_uname": "", "is_admin": 0,
		},
	}
	raw, _ := json.Marshal(payload)

	return raw
}

func dial(ctx context.Context, url string) (*websocket.Conn, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(4 << 20)

	return conn, nil
}

// waitPort 等网关监听端口就绪。
func waitPort(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}

	return false
}
