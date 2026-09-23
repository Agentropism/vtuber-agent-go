package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/archive"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"
)

// newChatStore 建一个带样例记录的归档：QQ 群两轮对话 + 一条点赞事件。
func newChatStore(t *testing.T) *archive.Store {
	t.Helper()

	store, err := archive.Open(filepath.Join(t.TempDir(), "chats"))
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	base := time.Date(2026, 9, 21, 23, 0, 0, 0, time.Local)
	records := []archive.Record{
		{Kind: archive.KindDialogue, Time: base, Platform: "qq", ChannelID: "group_1", EventKind: event.KindGroupMessage, UserName: "小明", Text: "你好", Reply: "你好呀"},
		{Kind: archive.KindDialogue, Time: base.Add(time.Minute), Platform: "qq", ChannelID: "group_1", EventKind: event.KindGroupMessage, UserName: "小红", Text: "在吗", Reply: "在的"},
		{Kind: archive.KindEvent, Time: base, Platform: "bilibili", ChannelID: "room_1", EventKind: event.KindLike, UserName: "观众甲", Text: "[点赞] x1"},
	}
	for _, record := range records {
		if err := store.Record(record); err != nil {
			t.Fatalf("写入归档: %v", err)
		}
	}

	return store
}

// 未配置归档时三个接口都应返回 503 而不是假装成功。
func TestChatsDisabled(t *testing.T) {
	handler := &Handler{}

	listRecorder := httptest.NewRecorder()
	handler.handleChats(listRecorder, newRequest(http.MethodGet, "/api/chats", ""))
	if listRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("列表状态码 = %d, want 503", listRecorder.Code)
	}

	recordsRecorder := httptest.NewRecorder()
	recordsReq := newRequest(http.MethodGet, "/api/chats/qq/group_1", "")
	recordsReq.SetPathValue("platform", "qq")
	recordsReq.SetPathValue("channel", "group_1")
	handler.handleChatRecords(recordsRecorder, recordsReq)
	if recordsRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("记录状态码 = %d, want 503", recordsRecorder.Code)
	}

	exportRecorder := httptest.NewRecorder()
	handler.handleChatsExport(exportRecorder, newRequest(http.MethodGet, "/api/chats/export?platform=qq", ""))
	if exportRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("导出状态码 = %d, want 503", exportRecorder.Code)
	}
}

// 列表接口按分类给出渠道摘要。
func TestChatsList(t *testing.T) {
	handler := &Handler{Chats: newChatStore(t)}

	recorder := httptest.NewRecorder()
	handler.handleChats(recorder, newRequest(http.MethodGet, "/api/chats", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	var body struct {
		Platforms []archive.PlatformInfo `json:"platforms"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(body.Platforms) != 2 {
		t.Fatalf("分类数 = %d, want 2: %s", len(body.Platforms), recorder.Body.String())
	}
	if body.Platforms[0].Platform != "bilibili" || body.Platforms[1].Platform != "qq" {
		t.Fatalf("分类顺序不符合预期: %+v", body.Platforms)
	}
	qq := body.Platforms[1].Channels
	if len(qq) != 1 || qq[0].ChannelID != "group_1" || qq[0].Count != 2 || qq[0].LastText != "在吗" {
		t.Fatalf("QQ 摘要不符合预期: %+v", qq)
	}
}

// 记录接口：新的在前、limit 截断、before 分页、未知渠道 404、坏参数 400。
func TestChatRecords(t *testing.T) {
	handler := &Handler{Chats: newChatStore(t)}

	do := func(query string, platform, channel string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := newRequest(http.MethodGet, "/api/chats/"+platform+"/"+channel+query, "")
		req.SetPathValue("platform", platform)
		req.SetPathValue("channel", channel)
		handler.handleChatRecords(recorder, req)
		return recorder
	}

	recorder := do("", "qq", "group_1")
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	var body struct {
		Count   int              `json:"count"`
		Records []archive.Record `json:"records"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if body.Count != 2 || body.Records[0].Text != "在吗" || body.Records[1].Text != "你好" {
		t.Fatalf("记录顺序不符合预期: %+v", body.Records)
	}

	// limit=1 只取最新一条
	limited := do("?limit=1", "qq", "group_1")
	if err := json.Unmarshal(limited.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if body.Count != 1 || body.Records[0].Text != "在吗" {
		t.Fatalf("limit 不符合预期: %+v", body.Records)
	}

	// before 取该时间之前的记录（RFC3339 里的 + 必须 URL 编码）
	before := url.QueryEscape(time.Date(2026, 9, 21, 23, 0, 30, 0, time.Local).Format(time.RFC3339))
	paged := do("?before="+before, "qq", "group_1")
	if err := json.Unmarshal(paged.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if body.Count != 1 || body.Records[0].Text != "你好" {
		t.Fatalf("before 不符合预期: %+v", body.Records)
	}

	if got := do("", "qq", "group_missing").Code; got != http.StatusNotFound {
		t.Fatalf("未知渠道状态码 = %d, want 404", got)
	}
	if got := do("?limit=abc", "qq", "group_1").Code; got != http.StatusBadRequest {
		t.Fatalf("坏 limit 状态码 = %d, want 400", got)
	}
	if got := do("?before=not-a-time", "qq", "group_1").Code; got != http.StatusBadRequest {
		t.Fatalf("坏 before 状态码 = %d, want 400", got)
	}
}

// 导出接口：jsonl/md 两种格式、下载头、缺失渠道与坏参数。
func TestChatsExport(t *testing.T) {
	handler := &Handler{Chats: newChatStore(t)}

	jsonlRecorder := httptest.NewRecorder()
	handler.handleChatsExport(jsonlRecorder, newRequest(http.MethodGet, "/api/chats/export?platform=qq&channel=group_1", ""))
	if jsonlRecorder.Code != http.StatusOK {
		t.Fatalf("导出 jsonl 状态码 = %d, want 200", jsonlRecorder.Code)
	}
	if disposition := jsonlRecorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "chat_qq_group_1.jsonl") {
		t.Fatalf("下载文件名不符合预期: %q", disposition)
	}
	if strings.Count(jsonlRecorder.Body.String(), "\n") != 2 {
		t.Fatalf("jsonl 应为两行: %q", jsonlRecorder.Body.String())
	}

	mdRecorder := httptest.NewRecorder()
	handler.handleChatsExport(mdRecorder, newRequest(http.MethodGet, "/api/chats/export?platform=qq&channel=group_1&format=md", ""))
	if mdRecorder.Code != http.StatusOK {
		t.Fatalf("导出 md 状态码 = %d, want 200", mdRecorder.Code)
	}
	if !strings.Contains(mdRecorder.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("md 内容类型不符合预期: %q", mdRecorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(mdRecorder.Body.String(), "Mili：你好呀") {
		t.Fatalf("md 内容不符合预期:\n%s", mdRecorder.Body.String())
	}

	if got := exportCode(handler, "?platform=qq&channel=group_missing"); got != http.StatusNotFound {
		t.Fatalf("缺失渠道状态码 = %d, want 404", got)
	}
	if got := exportCode(handler, "?channel=group_1"); got != http.StatusBadRequest {
		t.Fatalf("缺平台状态码 = %d, want 400", got)
	}
	if got := exportCode(handler, "?platform=qq&format=pdf"); got != http.StatusBadRequest {
		t.Fatalf("坏格式状态码 = %d, want 400", got)
	}
}

// exportCode 跑一次导出并返回状态码。
func exportCode(handler *Handler, query string) int {
	recorder := httptest.NewRecorder()
	handler.handleChatsExport(recorder, newRequest(http.MethodGet, "/api/chats/export"+query, ""))
	return recorder.Code
}
