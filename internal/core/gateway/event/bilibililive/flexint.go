package bilibililive

import (
	"fmt"
	"strconv"
	"strings"
)

// FlexibleInt64 兼容 JSON 整数与数字字符串,应对平台字段类型漂移。
// B 站开放平台部分字段(如 like_count)在整数与字符串之间漂移,
// Rust 端(bilibili-live de.rs)已有同义兼容;此处为 Go 侧等价实现。
type FlexibleInt64 int64

// UnmarshalJSON 接受数字(12)、引号数字("12")、空字符串与 null。
func (f *FlexibleInt64) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(strings.Trim(string(data), `"`))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("解析灵活整数 %q 失败: %w", s, err)
	}
	*f = FlexibleInt64(v)
	return nil
}
