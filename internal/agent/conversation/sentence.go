package conversation

import "strings"

// firstClauseMinLength 是「仅首句」允许在逗号处断开的最小字符数。
//
// 取值权衡：太小会把「好的，」这种两三字的碎片当作首句，听起来像是被噎住；
// 太大则退化成必须等句号，失去降低首字延迟的意义。5 个字符约等于一秒语音。
const firstClauseMinLength = 5

// Splitter 把流式文本按句切分，供逐句投递 TTS 使用。
//
// 切分规则：句末标点（。！？；… 与换行、以及半角 !?;）即断句；
// **仅首句**额外允许在逗号类停顿处断开——目的是让第一段音频尽早开始合成，
// 对应原实现的 faster_first_response。未闭合的尾部由 Flush 取出。
//
// 逐句投递是本包降低首字延迟的关键：不等整段回复生成完，第一句合成完即可开播，
// 也让播报队列的并行合成真正有活可干。
//
// Splitter 不是并发安全的，调用方需保证同一会话内串行使用。
type Splitter struct {
	buf          strings.Builder
	firstEmitted bool
}

// NewSplitter 构造切分器。
func NewSplitter() *Splitter {
	return &Splitter{}
}

// Write 追加一段流式文本，返回本次切分出的完整句子（可能为空）。
func (s *Splitter) Write(chunk string) []string {
	s.buf.WriteString(chunk)
	return s.drain(false)
}

// Flush 取出尾部残留（不足一句的部分）作为最后一句返回。
func (s *Splitter) Flush() []string {
	return s.drain(true)
}

// drain 切出缓冲区里的完整句子；final 为 true 时把剩余内容也作为一句返回。
func (s *Splitter) drain(final bool) []string {
	runes := []rune(s.buf.String())
	s.buf.Reset()

	var sentences []string
	start := 0

	for i, current := range runes {
		cut := isSentenceEnd(current)
		if !cut && !s.firstEmitted && isComma(current) && i-start+1 >= firstClauseMinLength {
			cut = true
		}
		if !cut {
			continue
		}

		if segment := strings.TrimSpace(string(runes[start : i+1])); segment != "" {
			sentences = append(sentences, segment)
			s.firstEmitted = true
		}
		start = i + 1
	}

	rest := string(runes[start:])
	if final {
		if segment := strings.TrimSpace(rest); segment != "" {
			sentences = append(sentences, segment)
		}
		return sentences
	}

	s.buf.WriteString(rest)
	return sentences
}

// isSentenceEnd 判断是否为句末标点。
func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '…', '\n', '!', '?', ';':
		return true
	default:
		return false
	}
}

// isComma 判断是否为逗号类停顿。
func isComma(r rune) bool {
	switch r {
	case '，', '、', ',':
		return true
	default:
		return false
	}
}
