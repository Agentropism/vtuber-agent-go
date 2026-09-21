package app

import (
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/event/bilibililive"
)

func TestBuildBilibiliNoticeText(t *testing.T) {
	like := buildBilibiliLikeNoticeText(bilibililive.LiveOpenPlatformLikeEvent{
		LikeText: "点赞用户点赞了",
	})
	if like != "[点赞] 点赞用户点赞了" {
		t.Fatalf("点赞通知文本错误: %q", like)
	}

	enter := buildBilibiliLiveRoomEnterNoticeText(bilibililive.LiveOpenPlatformLiveRoomEnterEvent{
		UName: "进入用户",
	})
	if enter != "[进入直播间] 进入用户" {
		t.Fatalf("进入直播间通知文本错误: %q", enter)
	}
}
