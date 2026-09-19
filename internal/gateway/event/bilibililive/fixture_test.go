package bilibililive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/shared/action"
)

// TestFixturesContract 用 bilibili-live(Rust)共享的 18 个事件 fixture 验证
// Go 端解析与分发契约等价:已知 cmd 全部可路由(非零 Action),未知 cmd 透传(零 Action)。
// 桩 handler 返回 cmd 名常量,与 data 内容无关,避免与 dispatch_test 注册顺序耦合。
func TestFixturesContract(t *testing.T) {
	registerFixtureStubs()

	dir := "fixtures"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 fixtures 目录失败: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("fixtures 目录为空,契约测试无输入")
	}

	unknown := map[string]bool{"unknown_cmd.json": true}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("读取 fixture %s 失败: %v", entry.Name(), err)
		}

		// 两种情况都要收尾:静默丢弃,故用闭包捕获 panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("fixture %s 分发 panic: %v", entry.Name(), r)
				}
			}()
			got := Dispatch(context.Background(), raw)
			if unknown[entry.Name()] {
				// 未知 cmd:透传为零 Action(与 Rust 端 Unknown 降级语义一致)
				if !isZeroAction(got) {
					t.Errorf("未知事件 %s 应透传零 Action,got=%+v", entry.Name(), got)
				}
				return
			}
			// 已知 cmd:必可路由到桩 handler(非零 Action)
			if isZeroAction(got) {
				t.Errorf("已知事件 %s 应可路由,got 零 Action", entry.Name())
			}
		}()
		checked++
	}
	t.Logf("fixtures 契约测试通过:共 %d 个 fixture", checked)
}

// registerFixtureStubs 注册与 dispatch_test 相同集合的桩 handler。
// 重复注册只会追加同值特性到链上,不影响「最后非零」语义。
func registerFixtureStubs() {
	stub := func(name string) func(context.Context, any) action.Action {
		return func(_ context.Context, _ any) action.Action {
			return action.Action{Action: name}
		}
	}
	LiveOpenPlatformDMActions.Add(func(ctx context.Context, e LiveOpenPlatformDMEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_DM")(ctx, e)
	})
	LiveOpenPlatformDMMirrorActions.Add(func(ctx context.Context, e LiveOpenPlatformDMMirrorEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_DM_MIRROR")(ctx, e)
	})
	LiveOpenPlatformSendGiftActions.Add(func(ctx context.Context, e LiveOpenPlatformSendGiftEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_SEND_GIFT")(ctx, e)
	})
	LiveOpenPlatformSuperChatActions.Add(func(ctx context.Context, e LiveOpenPlatformSuperChatEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_SUPER_CHAT")(ctx, e)
	})
	LiveOpenPlatformSuperChatDelActions.Add(func(ctx context.Context, e LiveOpenPlatformSuperChatDelEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL")(ctx, e)
	})
	LiveOpenPlatformGuardActions.Add(func(ctx context.Context, e LiveOpenPlatformGuardEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_GUARD")(ctx, e)
	})
	LiveOpenPlatformLikeActions.Add(func(ctx context.Context, e LiveOpenPlatformLikeEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_LIKE")(ctx, e)
	})
	LiveOpenPlatformLiveRoomEnterActions.Add(func(ctx context.Context, e LiveOpenPlatformLiveRoomEnterEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER")(ctx, e)
	})
	LiveOpenPlatformLiveStartActions.Add(func(ctx context.Context, e LiveOpenPlatformLiveStartEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_LIVE_START")(ctx, e)
	})
	LiveOpenPlatformLiveEndActions.Add(func(ctx context.Context, e LiveOpenPlatformLiveEndEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_LIVE_END")(ctx, e)
	})
	LiveOpenPlatformInteractionEndActions.Add(func(ctx context.Context, e LiveOpenPlatformInteractionEndEvent) action.Action {
		return stub("LIVE_OPEN_PLATFORM_INTERACTION_END")(ctx, e)
	})
}
