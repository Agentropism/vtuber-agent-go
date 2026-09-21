package api

import (
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
)

// durationType 用来把 config.Duration 渲染成 "5m" 这类可读字符串，而不是纳秒整数。
var durationType = reflect.TypeOf(config.Duration(0))

// secretKeys 是「密钥类」配置键：这些键不回值，只回是否已配置。
//
// 用精确匹配而不是子串：cookie_file 是路径、可以回，cookie 是凭据、不能回。
var secretKeys = map[string]bool{
	"api_key": true,
	"cookie":  true,
}

// handleConfig 返回当前生效的配置快照。
//
// 响应结构与 config.toml 同构（键名取自 toml tag），密钥类字段替换成
// {"configured": bool}——前端据此展示「有没有配」，明文永远不出后端。
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if h.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config unavailable")
		return
	}

	cfg, err := h.Config()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, structToMap(reflect.ValueOf(cfg).Elem()))
}

// structToMap 把配置结构体渲染成 map，键名用 toml tag（与配置文件一致）。
func structToMap(value reflect.Value) map[string]any {
	typ := value.Type()
	result := make(map[string]any, typ.NumField())

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("toml"), ",")[0]
		if name == "" {
			name = strings.ToLower(field.Name)
		}

		result[name] = fieldValue(name, value.Field(i))
	}

	return result
}

// fieldValue 渲染单个字段：密钥类只回是否已配置，其余按 JSON 友好形式递归。
func fieldValue(name string, value reflect.Value) any {
	if secretKeys[name] && value.Kind() == reflect.String {
		return map[string]bool{"configured": strings.TrimSpace(value.String()) != ""}
	}

	return jsonFriendly(value)
}

// jsonFriendly 把配置字段转成 JSON 能表达的值。
//
// 只需要覆盖配置里实际出现的种类：结构体（分区）、切片（引擎顺序）、
// 时间段（config.Duration）、基本类型。
func jsonFriendly(value reflect.Value) any {
	switch value.Kind() {
	case reflect.Struct:
		return structToMap(value)
	case reflect.Slice, reflect.Array:
		items := make([]any, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			items = append(items, jsonFriendly(value.Index(i)))
		}
		return items
	case reflect.Int64:
		if value.Type() == durationType {
			return time.Duration(value.Int()).String()
		}
		return value.Int()
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return nil
		}
		return jsonFriendly(value.Elem())
	default:
		return value.Interface()
	}
}
