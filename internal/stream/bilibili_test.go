package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// 签名的黄金值由 python 独立算得（hashlib.md5 + urlencode 排序）：
//
//	query = appkey=aae92bc66f3edfab&system_version=2&ts=1700000000000
//	sign  = md5(query + appsec)
//
// 用外部实现算一遍再钉进来，才不是拿自己的输出验自己。
func TestAppSignMatchesReferenceImplementation(t *testing.T) {
	got := appSign(map[string]string{"system_version": "2", "ts": "1700000000000"})

	want := "appkey=aae92bc66f3edfab&system_version=2&ts=1700000000000&sign=0145560363728c74c6e3f829a34d8991"
	if got != want {
		t.Fatalf("签名不符\n got: %s\nwant: %s", got, want)
	}
}

// 请求必须带齐 B 站要的字段与浏览器头，否则会被风控拒掉。
func TestStartLiveSendsSignedFormAndParsesAddress(t *testing.T) {
	var startBody url.Values
	var startHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xlive/app-blink/v1/liveVersionInfo/getHomePageLiveVersion":
			_, _ = w.Write([]byte(`{"code":0,"data":{"curr_version":"1.2.3","build":4567}}`))
		case "/room/v1/Room/startLive":
			startHeaders = r.Header.Clone()
			raw, _ := io.ReadAll(r.Body)
			startBody, _ = url.ParseQuery(string(raw))
			_, _ = w.Write([]byte(`{"code":0,"data":{"rtmp":{"addr":"rtmp://push.example/live-bvc/?streamname=abc","code":"deadbeef"}}}`))
		default:
			t.Errorf("意外的请求路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	info, err := StartLive(context.Background(), LiveConfig{
		Cookie:  "SESSDATA=s1; bili_jct=j1",
		RoomID:  12345,
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("开播: %v", err)
	}

	if info.Addr != "rtmp://push.example/live-bvc/?streamname=abc" || info.Key != "deadbeef" {
		t.Fatalf("推流信息解析错: %+v", info)
	}
	if want := "rtmp://push.example/live-bvc/?streamname=abc&key=deadbeef"; info.Output() != want {
		t.Fatalf("Output() = %q, want %q", info.Output(), want)
	}

	// 表单：开播必需项 + 签名
	for key, want := range map[string]string{
		"room_id":    "12345",
		"platform":   "pc_link",
		"csrf":       "j1", // 取自 cookie 里的 bili_jct
		"csrf_token": "j1",
		"version":    "1.2.3", // 必须来自 liveVersion 接口
		"build":      "4567",
	} {
		if got := startBody.Get(key); got != want {
			t.Fatalf("表单 %s = %q, want %q（整个表单: %v）", key, got, want, startBody)
		}
	}
	if startBody.Get("appkey") == "" || len(startBody.Get("sign")) != 32 {
		t.Fatalf("表单缺少 appkey/sign: %v", startBody)
	}
	if startBody.Get("ts") == "" {
		t.Fatal("表单缺少 ts")
	}

	// 头：登录态与浏览器特征
	if got := startHeaders.Get("Cookie"); !strings.Contains(got, "SESSDATA=s1") {
		t.Fatalf("Cookie 没有透传: %q", got)
	}
	if !strings.Contains(startHeaders.Get("User-Agent"), "Mozilla") {
		t.Fatalf("UA 不像浏览器: %q", startHeaders.Get("User-Agent"))
	}
	if startHeaders.Get("Origin") == "" {
		t.Fatal("缺少 Origin 头")
	}
	// 浏览器指纹头：B 站 风控按「像不像已知浏览器会话」决定是否套用严格准入
	for _, header := range []string{"Sec-Ch-Ua", "Sec-Ch-Ua-Platform", "Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site"} {
		if startHeaders.Get(header) == "" {
			t.Fatalf("缺少浏览器指纹头 %s", header)
		}
	}
}

// 需要扫码/人脸时要说人话，别只丢一个错误码。
// 需要扫码/实名时要把「验证入口」结构化地带出来，而不是只丢一句给人看的话。
func TestStartLiveSurfacesVerifyCode(t *testing.T) {
	cases := []struct {
		name     string
		code     int
		data     string
		wantQR   string
		wantFace bool
	}{
		{"需要扫码验证", 60024, `"data":{"qr":"https://example.com/qr"}`, "https://example.com/qr", false},
		{"需要实名认证", 60043, `"data":null`, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "startLive") {
					_, _ = w.Write([]byte(`{"code":` + itoa(tc.code) + `,"message":"need verify",` + tc.data + `}`))
					return
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"curr_version":"1.2.3","build":4567}}`))
			}))
			defer server.Close()

			_, err := StartLive(context.Background(), LiveConfig{
				Cookie:  "SESSDATA=s1; bili_jct=j1; DedeUserID=4987654",
				RoomID:  1,
				BaseURL: server.URL,
			})

			var liveErr *StartLiveError
			if !errors.As(err, &liveErr) {
				t.Fatalf("应当返回结构化错误，得到 %v", err)
			}
			if liveErr.Code != tc.code || !liveErr.NeedsUserAction() {
				t.Fatalf("错误码或类型不对: %+v", liveErr)
			}
			if liveErr.QR != tc.wantQR {
				t.Fatalf("扫码地址 = %q, want %q", liveErr.QR, tc.wantQR)
			}
			if tc.wantFace && !strings.Contains(liveErr.FaceAuth, "mid=4987654") {
				t.Fatalf("实名认证链接没带上 mid: %q", liveErr.FaceAuth)
			}
		})
	}
}

// 不认识的码（比如账号准入 60045）也要结构化返回；data 为 nil 时不能崩。
func TestStartLiveUnknownCodeWithNilData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "startLive") {
			_, _ = w.Write([]byte(`{"code":60045,"message":"未满足开播条件"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"curr_version":"1.2.3","build":4567}}`))
	}))
	defer server.Close()

	_, err := StartLive(context.Background(), LiveConfig{Cookie: "bili_jct=j1", RoomID: 1, BaseURL: server.URL})

	var liveErr *StartLiveError
	if !errors.As(err, &liveErr) {
		t.Fatalf("应当返回结构化错误，得到 %v", err)
	}
	if liveErr.Code != 60045 || liveErr.NeedsUserAction() {
		t.Fatalf("60045 不该被当成需要用户操作: %+v", liveErr)
	}
	if !strings.Contains(err.Error(), "未满足开播条件") {
		t.Fatalf("应当带上 B 站 的原话: %v", err)
	}
}

func TestStartLiveRejectsBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "startLive") {
			_, _ = w.Write([]byte(`{"code":-400,"message":"房间不存在"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"curr_version":"1.2.3","build":4567}}`))
	}))
	defer server.Close()

	_, err := StartLive(context.Background(), LiveConfig{Cookie: "bili_jct=j1", RoomID: 1, BaseURL: server.URL})
	if err == nil || !strings.Contains(err.Error(), "房间不存在") {
		t.Fatalf("应当把接口 message 带出来: %v", err)
	}
}

// 缺凭据/房间号时不能白跑一次请求（也别在真实环境里误开播）。
func TestStartLiveGuardsBeforeRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, err := StartLive(context.Background(), LiveConfig{RoomID: 1, BaseURL: server.URL}); err == nil {
		t.Fatal("没有 cookie 应当报错")
	}
	if _, err := StartLive(context.Background(), LiveConfig{Cookie: "bili_jct=j1", BaseURL: server.URL}); err == nil {
		t.Fatal("没有直播间号应当报错")
	}
	if called {
		t.Fatal("守卫应当在发请求之前拦住")
	}
}

func TestStopLivePostsCSRF(t *testing.T) {
	var body url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body, _ = url.ParseQuery(string(raw))
		_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer server.Close()

	if err := StopLive(context.Background(), LiveConfig{Cookie: "bili_jct=j1", RoomID: 99, BaseURL: server.URL}); err != nil {
		t.Fatalf("关播: %v", err)
	}
	if body.Get("room_id") != "99" || body.Get("csrf") != "j1" || body.Get("platform") != "pc_link" {
		t.Fatalf("关播表单不对: %v", body)
	}
}

// 冒烟里的假客户端也要能解析：地址没带问号时补 ?key=，没有 key 时不画蛇添足。
func TestLiveInfoOutput(t *testing.T) {
	cases := []struct {
		name string
		info LiveInfo
		want string
	}{
		{"带问号", LiveInfo{Addr: "rtmp://h/live/?streamname=a", Key: "k"}, "rtmp://h/live/?streamname=a&key=k"},
		{"不带问号", LiveInfo{Addr: "rtmp://h/live", Key: "k"}, "rtmp://h/live?key=k"},
		{"没有密钥", LiveInfo{Addr: "rtmp://h/live"}, "rtmp://h/live"},
	}
	for _, tc := range cases {
		if got := tc.info.Output(); got != tc.want {
			t.Fatalf("%s: Output() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// 响应形状变化时要报错而不是静默返回空地址。
func TestStartLiveRejectsEmptyAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "startLive") {
			_, _ = w.Write([]byte(`{"code":0,"message":"ok"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"curr_version": "1.2.3", "build": 4567},
		})
	}))
	defer server.Close()

	if _, err := StartLive(context.Background(), LiveConfig{Cookie: "bili_jct=j1", RoomID: 1, BaseURL: server.URL}); err == nil {
		t.Fatal("没有推流地址时应当报错")
	}
}
