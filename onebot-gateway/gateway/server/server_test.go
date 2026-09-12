package server

import (
	"context"
	"encoding/json"
	"testing"

	"go.uber.org/zap"
	"onebot-gateway/shared/action"
)

func TestSendActionForwardsStandardActionToConfiguredPlatformClient(t *testing.T) {
	var payload []byte
	client := registerClient("qq", &clientConnection{
		writeFunc: func(_ context.Context, got []byte) error {
			payload = append([]byte(nil), got...)
			return nil
		},
	}, zap.NewNop())
	t.Cleanup(func() { unregisterClient("qq", client) })

	want := action.Action{
		Action: "send_group_msg",
		Params: map[string]any{
			"group_id": float64(10001),
			"message":  "测试回调",
		},
		Echo: "callback-1",
	}
	if err := SendAction("qq", want); err != nil {
		t.Fatalf("转发 Action: %v", err)
	}

	var got action.Action
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("解析回调 Action: %v", err)
	}
	if got.Action != want.Action {
		t.Fatalf("action = %q, want %q", got.Action, want.Action)
	}
	if got.Echo != want.Echo {
		t.Fatalf("echo = %v, want %v", got.Echo, want.Echo)
	}
	params, ok := got.Params.(map[string]any)
	if !ok {
		t.Fatalf("params 类型 = %T, want map[string]any", got.Params)
	}
	if params["group_id"] != float64(10001) || params["message"] != "测试回调" {
		t.Fatalf("params = %#v, want 原始参数", params)
	}
}
