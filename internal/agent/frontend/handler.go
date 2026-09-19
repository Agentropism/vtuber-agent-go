package frontend

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/shared/emotion"

	"github.com/coder/websocket"
)

// webAssets 是内置的前端页面。嵌进二进制的原因：部署只要一个可执行文件，
// 不需要额外分发静态目录。
//
//go:embed all:web
var webAssets embed.FS

// Character 是前端要显示的角色信息。
type Character struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// Config 是前端配置。
type Config struct {
	Character Character
	// ModelName 是优先加载的 Live2D 模型名，对应角色文件里的 live2d_model。
	ModelName string
	// ModelsDir 是模型根目录，目录结构为 <模型名>/runtime/<模型名>.model3.json。
	ModelsDir string
	// ModelDict 是原项目的 model_dict.json，读得到就用它的缩放、偏移与 emotionMap。
	ModelDict string
	// ModelScale 覆盖模型清单里的显示倍率，0 表示不覆盖。
	ModelScale float64
}

// Frontend 是前端接入的对外门面。
type Frontend struct {
	cfg      Config
	hub      *hub
	model    modelInfo
	emotions *emotion.Map
}

// New 构造前端接入。
//
// 模型清单在这一步解析：读不到模型就直接报错，避免服务起来了却没人能出镜。
func New(cfg Config) (*Frontend, error) {
	entry, emotions, err := loadCatalog(cfg.ModelDict, cfg.ModelsDir, cfg.ModelName)
	if err != nil {
		return nil, err
	}

	model := modelInfo{
		Name:        entry.Name,
		URL:         entry.URL,
		Scale:       entry.Scale,
		XShift:      entry.InitialX,
		YShift:      entry.InitialY,
		IdleMotion:  entry.IdleMotion,
		Expressions: loadExpressionNames(cfg.ModelsDir, entry.URL),
	}
	if cfg.ModelScale > 0 {
		model.Scale = cfg.ModelScale
	}
	if model.Scale <= 0 {
		model.Scale = 1
	}

	log.Sugar().Infof("前端已就绪: 角色=%s 模型=%s 表情标签=%d 个 表达式=%d 个",
		cfg.Character.Name, model.Name, emotions.Len(), len(model.Expressions))

	return &Frontend{
		cfg:      cfg,
		hub:      newHub(),
		model:    model,
		emotions: emotions,
	}, nil
}

// Sink 返回播报投递实现，交给 broadcast.Queue 注入。
func (f *Frontend) Sink() *Sink {
	return NewSink(f.hub, f.emotions)
}

// ClientCount 返回当前连接的前端数量。
func (f *Frontend) ClientCount() int {
	return f.hub.count()
}

// Emotions 返回当前模型的表情词表。
//
// 对话侧用它把回复里的 [joy] 之类标签摘出来写进播报条目，
// 这样同一套词表只解析一次，前后端不会各写一份。
func (f *Frontend) Emotions() *emotion.Map {
	return f.emotions
}

// ClientWSHandler 处理 /client-ws：浏览器接入、收下行播报、回播报回执。
func (f *Frontend) ClientWSHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionDisabled,
		})
		if err != nil {
			log.Sugar().Warnf("前端 WebSocket 握手失败: %v", err)
			return
		}
		defer conn.CloseNow()

		client := f.hub.add(conn)
		defer f.hub.remove(client)

		if err := f.sendHello(r.Context(), client); err != nil {
			log.Sugar().Warnf("下发 hello 失败: %v", err)
			return
		}

		f.readLoop(r.Context(), conn)
	})
}

// ModelsHandler 处理 /live2d-models/：/info 返回清单，其余按静态文件返回。
func (f *Frontend) ModelsHandler() http.Handler {
	fileServer := http.StripPrefix("/live2d-models/", http.FileServer(http.Dir(f.cfg.ModelsDir)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSuffix(r.URL.Path, "/") == "/live2d-models/info" {
			f.writeModelInfo(w)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// WebHandler 提供内置的前端页面。
//
// 单独挂在 /web/ 而不是 /：接入客户端可以占用根路径（配置里的 path），
// 前端与它们的路由不该互相打架。
func (f *Frontend) WebHandler() http.Handler {
	sub, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Sugar().Errorf("前端资源不可用: %v", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "前端资源不可用", http.StatusInternalServerError)
		})
	}

	return http.StripPrefix("/web/", http.FileServer(http.FS(sub)))
}

// FaviconHandler 回应浏览器自动请求的 /favicon.ico。
//
// 不处理的话这个请求会落到接入客户端的根路径 WebSocket 路由上（[[clients]].path
// 常配成 "/"），被当成非法的升级请求而返回 426，浏览器控制台里一直挂着一条报错。
// 这里明确回 204：没有图标，但请求是被正常处理的。
func (f *Frontend) FaviconHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

// sendHello 把角色与模型信息下发给刚连上的浏览器。
func (f *Frontend) sendHello(ctx context.Context, client *client) error {
	payload, err := json.Marshal(message{
		Type: "hello",
		Data: map[string]any{
			"character": f.cfg.Character,
			"model":     f.model,
		},
	})
	if err != nil {
		return err
	}

	return client.write(ctx, payload)
}

// readLoop 处理浏览器上行消息。
func (f *Frontend) readLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var msg clientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Sugar().Warnf("解析前端消息失败: %v", err)
			continue
		}

		switch msg.Type {
		case "playback-started":
			var seq seqData
			if err := json.Unmarshal(msg.Data, &seq); err == nil {
				log.Sugar().Debugf("前端开始播放: seq=%d", seq.Seq)
			}
		case "playback-finished":
			var seq seqData
			if err := json.Unmarshal(msg.Data, &seq); err != nil {
				log.Sugar().Warnf("解析播报回执失败: %v", err)
				continue
			}
			f.hub.notifyPlayback(seq.Seq)
		default:
			log.Sugar().Debugf("忽略前端消息: %s", msg.Type)
		}
	}
}

// modelInfoCharacter 是模型清单里的角色条目，字段沿用原项目的命名。
type modelInfoCharacter struct {
	Name      string `json:"name"`
	Avatar    string `json:"avatar"`
	ModelPath string `json:"model_path"`
}

// modelInfoResponse 是 /live2d-models/info 的响应体。
type modelInfoResponse struct {
	Type       string               `json:"type"`
	Count      int                  `json:"count"`
	Characters []modelInfoCharacter `json:"characters"`
}

func (f *Frontend) writeModelInfo(w http.ResponseWriter) {
	response := modelInfoResponse{
		Type:  "live2d-models",
		Count: 1,
		Characters: []modelInfoCharacter{{
			Name:      f.model.Name,
			Avatar:    f.cfg.Character.Avatar,
			ModelPath: f.model.URL,
		}},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Sugar().Warnf("写出模型清单失败: %v", err)
	}
}
