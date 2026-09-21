package event

import "testing"

func TestNeedsReply(t *testing.T) {
	needsReply := []Kind{
		KindGroupMessage,
		KindDanmaku,
		KindSuperChat,
		KindGift,
		KindGuard,
	}
	for _, kind := range needsReply {
		if !kind.NeedsReply() {
			t.Errorf("%s 应需要回复", kind)
		}
		if !kind.IsKnown() {
			t.Errorf("%s 应是已声明的种类", kind)
		}
	}

	noReply := []Kind{
		KindNotice,
		KindLike,
		KindLiveRoomEnter,
		KindLiveStart,
		KindLiveEnd,
	}
	for _, kind := range noReply {
		if kind.NeedsReply() {
			t.Errorf("%s 不应需要回复", kind)
		}
		if !kind.IsKnown() {
			t.Errorf("%s 应是已声明的种类", kind)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	unknown := Kind("不存在的种类")
	if unknown.IsKnown() {
		t.Error("未声明的种类不应被认作已知")
	}
	if unknown.NeedsReply() {
		t.Error("未声明的种类不应需要回复")
	}
	if unknown.String() != "不存在的种类" {
		t.Errorf("String 应原样返回: %q", unknown.String())
	}
}
