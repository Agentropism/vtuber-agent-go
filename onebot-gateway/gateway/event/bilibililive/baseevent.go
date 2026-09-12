package bilibililive

// OpenPlatformPacket 是 B 站直播开放平台长链推送的通用包结构。
type OpenPlatformPacket[T any] struct {
	Cmd  string `json:"cmd"`
	Data T      `json:"data"`
}
