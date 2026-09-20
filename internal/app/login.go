package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/config"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/server"
	"github.com/Agentropism/vtuber-agent-go/internal/stream"

	"github.com/skip2/go-qrcode"
	"go.uber.org/zap"
)

// 扫码登录：浏览器打开 /login/ 扫一张二维码，登录态落到 cookie 文件里，
// 推流与开播接口直接复用（见 stream.go 的 cookie 来源顺序）。
//
// 三个端点全部**只允许本机访问**（回环地址）：二维码一被扫就绑定账号，
// 把开播权限暴露到局域网没有任何理由。
const (
	loginPagePattern   = "/login/"
	loginQRCodePattern = "/login/qrcode.png"
	loginStatusPattern = "/login/status"

	// loginQRCodeTTL 是二维码自认为的新鲜期；B 站 侧的过期时间约 180s，
	// 这里留一点余量，过期后重新申请。
	loginQRCodeTTL = 150 * time.Second
	// loginQRCodeSize 是二维码图片边长（像素），够手机扫又不至于糊。
	loginQRCodeSize = 320
	// defaultCookieFile 是登录态默认落盘位置。
	defaultCookieFile = "data/bilibili-cookie.txt"
)

// loginService 持有当前二维码会话。
//
// 同一时刻只保留一张二维码：多人扫同一张会让 cookie 归属不清。
type loginService struct {
	cfg config.StreamConfig
	log *zap.Logger
	// api 指向 B 站 passport 接口；测试注入 httptest 地址，生产留空用官方地址。
	api stream.LoginConfig

	mu      sync.Mutex
	qr      stream.LoginQRCode
	qrAt    time.Time
	cookie  string
	savedAt time.Time
}

// provideLogin 装配扫码登录；未启用推流时返回 nil（登录只为拿开播凭据，别的地方用不上）。
func provideLogin(cfg *config.Config, log *zap.Logger) *loginService {
	if !cfg.Stream.Enabled {
		return nil
	}

	return &loginService{
		cfg: cfg.Stream,
		log: log,
		// 启动时先把已登录的凭据读进来，省一次"未登录"的误报
		cookie: stream.LoadLoginCookie(cookiePath(cfg.Stream)),
	}
}

// cookiePath 返回登录态落盘路径。
func cookiePath(s config.StreamConfig) string {
	if strings.TrimSpace(s.CookieFile) != "" {
		return s.CookieFile
	}

	return defaultCookieFile
}

// Cookie 返回可用的登录态；没有则返回空串。
func (s *loginService) Cookie() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cookie
}

// routes 返回登录页与它的两个数据端点。
func (s *loginService) routes() []server.Route {
	return []server.Route{
		{Pattern: loginPagePattern, Handler: loopbackOnly(loginPageHandler())},
		{Pattern: loginQRCodePattern, Handler: loopbackOnly(s.qrcodeHandler())},
		{Pattern: loginStatusPattern, Handler: loopbackOnly(s.statusHandler())},
	}
}

// qrcodeHandler 出二维码 PNG；没有新鲜二维码时先申请一张。
func (s *loginService) qrcodeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		qr, err := s.ensureQRCodeLocked(r.Context())
		s.mu.Unlock()

		if err != nil {
			http.Error(w, "申请二维码失败: "+err.Error(), http.StatusBadGateway)
			return
		}

		png, err := qrcode.Encode(qr.URL, qrcode.Medium, loginQRCodeSize)
		if err != nil {
			http.Error(w, "生成二维码失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})
}

// statusHandler 轮询一次登录状态；确认就把凭据落盘。
func (s *loginService) statusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		qr, err := s.ensureQRCodeLocked(r.Context())
		s.mu.Unlock()
		if err != nil {
			writeLoginStatus(w, loginStatusPayload{State: "error", Message: err.Error()})
			return
		}

		result, err := stream.PollLogin(r.Context(), s.api, qr.Key)
		if err != nil {
			writeLoginStatus(w, loginStatusPayload{State: "error", Message: err.Error()})
			return
		}

		if result.State == stream.LoginExpired {
			// 作废本地二维码，下次请求会重新申请
			s.mu.Lock()
			s.qr = stream.LoginQRCode{}
			s.mu.Unlock()
		}

		if result.State == stream.LoginConfirmed {
			if err := s.storeCookie(result.Cookie); err != nil {
				writeLoginStatus(w, loginStatusPayload{State: "error", Message: err.Error()})
				return
			}
		}

		writeLoginStatus(w, loginStatusPayload{State: string(result.State), Message: result.Message})
	})
}

// storeCookie 记住登录态并落盘（内存 + 文件，推流侧读文件）。
func (s *loginService) storeCookie(cookie string) error {
	path := cookiePath(s.cfg)
	if err := stream.SaveLoginCookie(path, cookie); err != nil {
		return fmt.Errorf("保存登录态: %w", err)
	}

	s.mu.Lock()
	s.cookie = cookie
	s.savedAt = time.Now()
	s.mu.Unlock()

	// 只记来源与长度，绝不把凭据写进日志
	s.log.Sugar().Infof("扫码登录成功，登录态已保存: %s（%d 字符）", path, len(cookie))

	return nil
}

// ensureQRCodeLocked 保证有一张新鲜的二维码；调用方需持锁。
func (s *loginService) ensureQRCodeLocked(ctx context.Context) (stream.LoginQRCode, error) {
	if s.qr.Key != "" && time.Since(s.qrAt) < loginQRCodeTTL {
		return s.qr, nil
	}

	qr, err := stream.GenerateLoginQRCode(ctx, s.api)
	if err != nil {
		return stream.LoginQRCode{}, err
	}

	s.qr = qr
	s.qrAt = time.Now()
	s.log.Sugar().Infof("已申请登录二维码，请在浏览器打开 http://<本机>/login/ 扫码")

	return qr, nil
}

// loginStatusPayload 是 /login/status 的响应体。
type loginStatusPayload struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

func writeLoginStatus(w http.ResponseWriter, payload loginStatusPayload) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}

// loopbackOnly 只放行本机请求。
//
// 这台机器上的浏览器访问 127.0.0.1 就能用；局域网里能访问到就意味着别人能拿
// 你的账号开播，所以直接拒掉。RemoteAddr 是 TCP 对端地址，伪造不了。
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			http.Error(w, "登录页只允许本机访问", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))

	return ip != nil && ip.IsLoopback()
}

// loginPageHandler 出登录页。页面轮询 /login/status 并把状态写在二维码下面。
func loginPageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != loginPagePattern {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(loginPageHTML))
	})
}

// loginPageHTML 是自带样式的最小登录页，无外部依赖（二维码由后端出 PNG）。
const loginPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>B 站扫码登录</title>
<style>
  body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
         background: #14161a; color: #e8eaf0; font: 14px/1.6 system-ui, -apple-system, "Noto Sans SC", sans-serif; }
  .card { background: #1c1f26; border: 1px solid #2a2f3a; border-radius: 14px; padding: 28px 32px; text-align: center; }
  h1 { margin: 0 0 4px; font-size: 17px; font-weight: 600; }
  .hint { margin: 0 0 18px; color: #8b93a7; font-size: 13px; }
  .qr { width: 240px; height: 240px; background: #fff; border-radius: 10px; padding: 10px; display: block; }
  .status { margin-top: 16px; font-size: 13px; color: #8b93a7; min-height: 20px; }
  .status.ok { color: #4ade80; }
  .status.err { color: #f87171; }
  button { margin-top: 12px; background: #2a2f3a; color: #e8eaf0; border: 0; border-radius: 8px;
           padding: 8px 16px; font-size: 13px; cursor: pointer; display: none; }
  button:hover { background: #343a48; }
</style>
</head>
<body>
  <div class="card">
    <h1>B 站扫码登录</h1>
    <p class="hint">用哔哩哔哩 App 扫码，登录态会保存到本机</p>
    <img class="qr" id="qr" src="/login/qrcode.png" alt="登录二维码">
    <div class="status" id="status">加载中…</div>
    <button id="refresh">换一张</button>
  </div>
<script>
(function () {
  var qr = document.getElementById('qr');
  var statusEl = document.getElementById('status');
  var refresh = document.getElementById('refresh');
  var timer = null;
  var done = false;

  function setStatus(text, kind) {
    statusEl.textContent = text;
    statusEl.className = 'status' + (kind ? ' ' + kind : '');
  }

  function reloadQRCode() {
    done = false;
    refresh.style.display = 'none';
    setStatus('加载中…');
    qr.src = '/login/qrcode.png?t=' + Date.now();
    if (timer) { clearInterval(timer); }
    timer = setInterval(poll, 1500);
  }

  function poll() {
    if (done) { return; }
    fetch('/login/status', { cache: 'no-store' })
      .then(function (res) { return res.json(); })
      .then(function (data) {
        switch (data.state) {
          case 'waiting': setStatus('请用哔哩哔哩 App 扫码'); break;
          case 'scanned': setStatus('已扫码，请在手机上确认登入'); break;
          case 'confirmed':
            done = true;
            clearInterval(timer);
            qr.style.display = 'none';
            setStatus('登录成功，可以关掉这个页面了', 'ok');
            break;
          case 'expired':
            done = true;
            clearInterval(timer);
            setStatus('二维码已过期', 'err');
            refresh.style.display = 'inline-block';
            break;
          default:
            setStatus(data.message || '登录失败', 'err');
            refresh.style.display = 'inline-block';
        }
      })
      .catch(function (err) { setStatus('请求失败：' + err.message, 'err'); });
  }

  refresh.addEventListener('click', reloadQRCode);
  qr.addEventListener('error', function () { setStatus('二维码加载失败', 'err'); });
  reloadQRCode();
})();
</script>
</body>
</html>
`

// errLoginDisabled 在未启用推流时由调用方使用（保持错误文案一致）。
var errLoginDisabled = errors.New("未启用 [stream]，登录页不可用")
