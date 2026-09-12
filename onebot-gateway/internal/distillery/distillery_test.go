package distillery

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSendPostsUnifiedEvent(t *testing.T) {
	want := UnifiedEvent{
		Platform:    "bilibili",
		UserID:      "open_1",
		UserName:    "测试用户",
		GroupID:     "room_123456",
		Content:     "火箭×1",
		MessageType: "gift",
	}

	originalClient := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Errorf("请求方法错误: %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type 错误: %s", got)
		}

		var got UnifiedEvent
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("解析请求失败: %v", err)
		}
		if got != want {
			t.Errorf("请求事件错误: got=%#v want=%#v", got, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { httpClient = originalClient })

	if err := send("http://distillery.test/event", want); err != nil {
		t.Fatalf("发送事件失败: %v", err)
	}
}

func TestSendReportsNonSuccessStatus(t *testing.T) {
	originalClient := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { httpClient = originalClient })

	err := send("http://distillery.test/event", UnifiedEvent{})
	if err == nil {
		t.Fatal("非成功状态应返回错误")
	}
}
