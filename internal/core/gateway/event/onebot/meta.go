package onebot

type MetaLifecycleEvent struct {
	BaseEvent
	MetaEventType string `json:"meta_event_type"`
	SubType       string `json:"sub_type"`
}

type Status struct {
	Online bool `json:"online"`
	Good   bool `json:"good"`
}

type MetaHeartbeatEvent struct {
	BaseEvent
	MetaEventType string `json:"meta_event_type"`
	Status        Status `json:"status"`
	Interval      int64  `json:"interval"`
}
