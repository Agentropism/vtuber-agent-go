package onebot

import "encoding/json"

type PrivateSender struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Card     string `json:"card,omitempty"`
	Sex      string `json:"sex"`
	Age      int32  `json:"age"`
}

type GroupSender struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Card     string `json:"card"`
	Sex      string `json:"sex"`
	Age      int32  `json:"age"`
	Area     string `json:"area"`
	Level    string `json:"level"`
	Role     string `json:"role"`
	Title    string `json:"title"`
}

type Anonymous struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Flag string `json:"flag"`
}

type MessagePrivateEvent struct {
	BaseEvent
	MessageType string          `json:"message_type"`
	SubType     string          `json:"sub_type"`
	MessageID   int64           `json:"message_id"`
	UserID      int64           `json:"user_id"`
	Message     json.RawMessage `json:"message"`
	RawMessage  string          `json:"raw_message"`
	Font        int32           `json:"font"`
	Sender      PrivateSender   `json:"sender"`

	MessageSeq    int64  `json:"message_seq,omitempty"`
	RealID        int64  `json:"real_id,omitempty"`
	RealSeq       string `json:"real_seq,omitempty"`
	MessageFormat string `json:"message_format,omitempty"`
	TargetID      int64  `json:"target_id,omitempty"`
}

type MessageGroupEvent struct {
	BaseEvent
	MessageType string          `json:"message_type"`
	SubType     string          `json:"sub_type"`
	MessageID   int64           `json:"message_id"`
	GroupID     int64           `json:"group_id"`
	UserID      int64           `json:"user_id"`
	Anonymous   *Anonymous      `json:"anonymous"`
	Message     json.RawMessage `json:"message"`
	RawMessage  string          `json:"raw_message"`
	Font        int32           `json:"font"`
	Sender      GroupSender     `json:"sender"`

	MessageSeq    int64  `json:"message_seq,omitempty"`
	RealID        int64  `json:"real_id,omitempty"`
	RealSeq       string `json:"real_seq,omitempty"`
	MessageFormat string `json:"message_format,omitempty"`
	GroupName     string `json:"group_name,omitempty"`
}
