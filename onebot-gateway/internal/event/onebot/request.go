package onebot

type RequestFriendEvent struct {
	BaseEvent
	RequestType string `json:"request_type"`
	UserID      int64  `json:"user_id"`
	Comment     string `json:"comment"`
	Flag        string `json:"flag"`
}

type RequestGroupEvent struct {
	BaseEvent
	RequestType string `json:"request_type"`
	SubType     string `json:"sub_type"`
	GroupID     int64  `json:"group_id"`
	UserID      int64  `json:"user_id"`
	Comment     string `json:"comment"`
	Flag        string `json:"flag"`
}
