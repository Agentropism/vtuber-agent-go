package onebot

type MessageSegment struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}
