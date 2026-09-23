package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// 日志接口：透传 limit 与 level，返回缓冲快照。
func TestLogsList(t *testing.T) {
	var gotLimit int
	var gotLevel string

	handler := &Handler{Logs: func(limit int, level string) []logger.Entry {
		gotLimit, gotLevel = limit, level
		return []logger.Entry{{Time: time.Now(), Level: "WARN", Msg: "警告一条"}}
	}}

	recorder := httptest.NewRecorder()
	handler.handleLogs(recorder, newRequest(http.MethodGet, "/api/logs?limit=50&level=warn", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}
	if gotLimit != 50 || gotLevel != "warn" {
		t.Fatalf("查询参数不符合预期: limit=%d level=%q", gotLimit, gotLevel)
	}

	var body struct {
		Count   int            `json:"count"`
		Entries []logger.Entry `json:"entries"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if body.Count != 1 || body.Entries[0].Msg != "警告一条" {
		t.Fatalf("响应内容不符合预期: %s", recorder.Body.String())
	}
}

// 不带参数用默认条数；坏 limit 400；未装配 503。
func TestLogsDefaultsAndRejections(t *testing.T) {
	gotLimit := -1
	handler := &Handler{Logs: func(limit int, level string) []logger.Entry {
		gotLimit = limit
		return nil
	}}

	recorder := httptest.NewRecorder()
	handler.handleLogs(recorder, newRequest(http.MethodGet, "/api/logs", ""))
	if recorder.Code != http.StatusOK || gotLimit != logger.DefaultQueryLimit {
		t.Fatalf("默认条数不符合预期: code=%d limit=%d", recorder.Code, gotLimit)
	}

	bad := httptest.NewRecorder()
	handler.handleLogs(bad, newRequest(http.MethodGet, "/api/logs?limit=abc", ""))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("坏 limit 状态码 = %d, want 400", bad.Code)
	}

	disabled := httptest.NewRecorder()
	(&Handler{}).handleLogs(disabled, newRequest(http.MethodGet, "/api/logs", ""))
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("未装配状态码 = %d, want 503", disabled.Code)
	}
}
