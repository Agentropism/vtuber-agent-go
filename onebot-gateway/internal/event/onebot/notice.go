package onebot

type GroupUploadFile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	BusID int64  `json:"busid"`
}

type NoticeGroupUploadEvent struct {
	BaseEvent
	NoticeType string          `json:"notice_type"`
	GroupID    int64           `json:"group_id"`
	UserID     int64           `json:"user_id"`
	File       GroupUploadFile `json:"file"`
}

type NoticeGroupAdminEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	UserID     int64  `json:"user_id"`
}

type NoticeGroupDecreaseEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	OperatorID int64  `json:"operator_id"`
	UserID     int64  `json:"user_id"`
}

type NoticeGroupIncreaseEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	OperatorID int64  `json:"operator_id"`
	UserID     int64  `json:"user_id"`
}

type NoticeGroupBanEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	OperatorID int64  `json:"operator_id"`
	UserID     int64  `json:"user_id"`
	Duration   int64  `json:"duration"`
}

type NoticeFriendAddEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	UserID     int64  `json:"user_id"`
}

type NoticeGroupRecallEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	GroupID    int64  `json:"group_id"`
	UserID     int64  `json:"user_id"`
	OperatorID int64  `json:"operator_id"`
	MessageID  int64  `json:"message_id"`
}

type NoticeFriendRecallEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	UserID     int64  `json:"user_id"`
	MessageID  int64  `json:"message_id"`
}

type NoticeNotifyPokeEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	UserID     int64  `json:"user_id"`
	TargetID   int64  `json:"target_id"`
}

type NoticeNotifyLuckyKingEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	UserID     int64  `json:"user_id"`
	TargetID   int64  `json:"target_id"`
}

type NoticeNotifyHonorEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	HonorType  string `json:"honor_type"`
	UserID     int64  `json:"user_id"`
}

// NoticeNotifyInputStatusEvent is a NapCat extension to OneBot 11.
type NoticeNotifyInputStatusEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	StatusText string `json:"status_text"`
	EventType  int    `json:"event_type"`
	UserID     int64  `json:"user_id"`
	GroupID    int64  `json:"group_id"`
}
