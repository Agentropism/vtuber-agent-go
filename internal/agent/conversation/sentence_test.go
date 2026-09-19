package conversation

import (
	"strings"
	"testing"
)

func TestSplitterSplitsOnSentenceEnd(t *testing.T) {
	splitter := NewSplitter()

	got := splitter.Write("你好。在吗？我在这儿！")
	want := []string{"你好。", "在吗？", "我在这儿！"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("切分结果不符合预期:\n实际: %v\n期望: %v", got, want)
	}
	if rest := splitter.Flush(); len(rest) != 0 {
		t.Errorf("全部断句后不应有残留: %v", rest)
	}
}

// TestSplitterWorksAcrossChunks 覆盖流式场景：标点可能落在任意分片边界上。
func TestSplitterWorksAcrossChunks(t *testing.T) {
	splitter := NewSplitter()

	var got []string
	for _, chunk := range []string{"你", "好。", "在", "吗", "？", "在的"} {
		got = append(got, splitter.Write(chunk)...)
	}
	got = append(got, splitter.Flush()...)

	want := []string{"你好。", "在吗？", "在的"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("跨分片切分不符合预期:\n实际: %v\n期望: %v", got, want)
	}
}

// TestSplitterSplitsFirstClauseOnComma 覆盖 faster_first_response：
// 首句在逗号处提前断开，让第一段音频尽早开始合成。
func TestSplitterSplitsFirstClauseOnComma(t *testing.T) {
	splitter := NewSplitter()

	got := splitter.Write("今天天气不错，我们出去走走吧。")
	want := []string{"今天天气不错，", "我们出去走走吧。"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("首句逗号断句不符合预期: %v", got)
	}
}

// TestSplitterOnlySplitsFirstClauseOnComma 只有首句享受逗号断句。
func TestSplitterOnlySplitsFirstClauseOnComma(t *testing.T) {
	splitter := NewSplitter()

	got := splitter.Write("第一句话。第二句话很长很长，不应该在逗号处断开。")
	want := []string{"第一句话。", "第二句话很长很长，不应该在逗号处断开。"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("非首句不应按逗号断开: %v", got)
	}
}

func TestSplitterIgnoresShortFirstClause(t *testing.T) {
	splitter := NewSplitter()

	// 逗号前不足 firstClauseMinLength 个字符时不断开
	got := splitter.Write("好的，我这就来。")
	want := []string{"好的，我这就来。"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("过短的首句不应按逗号断开: %v", got)
	}
}

func TestSplitterHandlesEnglishAndNewlines(t *testing.T) {
	splitter := NewSplitter()

	got := splitter.Write("Hello! Are you there?")
	want := []string{"Hello!", "Are you there?"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("半角标点切分不符合预期: %v", got)
	}

	// 换行也是断句点；末尾没有标点的部分要等 Flush
	got = splitter.Write("第一行\n第二行")
	if len(got) != 1 || got[0] != "第一行" {
		t.Fatalf("换行切分不符合预期: %v", got)
	}
	if rest := splitter.Flush(); len(rest) != 1 || rest[0] != "第二行" {
		t.Fatalf("换行后的残留不符合预期: %v", rest)
	}
}

func TestSplitterFlushReturnsRemainder(t *testing.T) {
	splitter := NewSplitter()

	if got := splitter.Write("还没说完"); len(got) != 0 {
		t.Fatalf("未断句时不应产出: %v", got)
	}

	got := splitter.Flush()
	if len(got) != 1 || got[0] != "还没说完" {
		t.Fatalf("Flush 应返回残留: %v", got)
	}
	if again := splitter.Flush(); len(again) != 0 {
		t.Errorf("Flush 之后应清空: %v", again)
	}
}

func TestSplitterIgnoresBlankChunks(t *testing.T) {
	splitter := NewSplitter()

	if got := splitter.Write(""); len(got) != 0 {
		t.Errorf("空分片不应产出: %v", got)
	}
	if got := splitter.Write("   \n  "); len(got) != 0 {
		t.Errorf("纯空白不应产出: %v", got)
	}
	if got := splitter.Flush(); len(got) != 0 {
		t.Errorf("纯空白 Flush 不应产出: %v", got)
	}
}

func TestSplitterPreservesFullText(t *testing.T) {
	const source = "第一句话。第二句话，还有后续；最后一句没有标点"

	splitter := NewSplitter()
	var parts []string
	parts = append(parts, splitter.Write(source)...)
	parts = append(parts, splitter.Flush()...)

	// 逐句投递不应丢字：拼回去应与原文一致（允许调用方自己 trim 空白）
	joined := strings.Join(parts, "")
	if strings.ReplaceAll(joined, " ", "") != strings.ReplaceAll(source, " ", "") {
		t.Fatalf("切分后丢字:\n原文: %q\n拼接: %q", source, joined)
	}
}
