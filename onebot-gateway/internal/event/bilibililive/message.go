package bilibililive

const (
	// CmdLiveOpenPlatformDM 表示直播间弹幕消息。
	CmdLiveOpenPlatformDM             = "LIVE_OPEN_PLATFORM_DM"
	CmdLiveOpenPlatformDMMirror       = "LIVE_OPEN_PLATFORM_DM_MIRROR"
	CmdLiveOpenPlatformSendGift       = "LIVE_OPEN_PLATFORM_SEND_GIFT"
	CmdLiveOpenPlatformSuperChat      = "LIVE_OPEN_PLATFORM_SUPER_CHAT"
	CmdLiveOpenPlatformSuperChatDel   = "LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL"
	CmdLiveOpenPlatformGuard          = "LIVE_OPEN_PLATFORM_GUARD"
	CmdLiveOpenPlatformLike           = "LIVE_OPEN_PLATFORM_LIKE"
	CmdLiveOpenPlatformLiveRoomEnter  = "LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER"
	CmdLiveOpenPlatformLiveStart      = "LIVE_OPEN_PLATFORM_LIVE_START"
	CmdLiveOpenPlatformLiveEnd        = "LIVE_OPEN_PLATFORM_LIVE_END"
	CmdLiveOpenPlatformInteractionEnd = "LIVE_OPEN_PLATFORM_INTERACTION_END"
	CmdStatus                         = "status"
)

// LiveOpenPlatformDMEvent 是 B 站直播开放平台弹幕事件。
type LiveOpenPlatformDMEvent struct {
	RoomID                 int64   `json:"room_id"`
	UID                    int64   `json:"uid"`
	OpenID                 string  `json:"open_id"`
	UnionID                *string `json:"union_id,omitempty"`
	UName                  string  `json:"uname"`
	Message                string  `json:"msg"`
	MessageID              string  `json:"msg_id"`
	FansMedalLevel         int64   `json:"fans_medal_level"`
	FansMedalName          string  `json:"fans_medal_name"`
	FansMedalWearingStatus bool    `json:"fans_medal_wearing_status"`
	GuardLevel             int64   `json:"guard_level"`
	Timestamp              int64   `json:"timestamp"`
	UFace                  string  `json:"uface"`
	EmojiImageURL          string  `json:"emoji_img_url"`
	DMType                 int64   `json:"dm_type"`
	GloryLevel             int64   `json:"glory_level"`
	ReplyOpenID            string  `json:"reply_open_id"`
	ReplyUName             string  `json:"reply_uname"`
	IsAdmin                int64   `json:"is_admin"`
}

// LiveOpenPlatformDMMirrorEvent 是弹幕镜像事件。
type LiveOpenPlatformDMMirrorEvent struct {
	Timestamp     int64  `json:"timestamp"`
	RoomID        int64  `json:"room_id"`
	Message       string `json:"msg"`
	MessageID     string `json:"msg_id"`
	EmojiImageURL string `json:"emoji_img_url"`
	DMType        int64  `json:"dm_type"`
}

// LiveOpenPlatformSendGiftEvent 是礼物事件。
type LiveOpenPlatformSendGiftEvent struct {
	RoomID                 int64                               `json:"room_id"`
	UID                    int64                               `json:"uid"`
	OpenID                 string                              `json:"open_id"`
	UnionID                *string                             `json:"union_id,omitempty"`
	UName                  string                              `json:"uname"`
	UFace                  string                              `json:"uface"`
	GiftID                 int64                               `json:"gift_id"`
	GiftName               string                              `json:"gift_name"`
	GiftNum                int64                               `json:"gift_num"`
	Price                  int64                               `json:"price"`
	RPrice                 int64                               `json:"r_price"`
	Paid                   bool                                `json:"paid"`
	FansMedalLevel         int64                               `json:"fans_medal_level"`
	FansMedalName          string                              `json:"fans_medal_name"`
	FansMedalWearingStatus bool                                `json:"fans_medal_wearing_status"`
	GuardLevel             int64                               `json:"guard_level"`
	Timestamp              int64                               `json:"timestamp"`
	AnchorInfo             *LiveOpenPlatformSendGiftAnchorInfo `json:"anchor_info,omitempty"`
	MessageID              string                              `json:"msg_id"`
	GiftIcon               string                              `json:"gift_icon"`
	ComboGift              bool                                `json:"combo_gift"`
	ComboInfo              *LiveOpenPlatformSendGiftComboInfo  `json:"combo_info,omitempty"`
	BlindGift              *LiveOpenPlatformSendGiftBlindGift  `json:"blind_gift,omitempty"`
}

// LiveOpenPlatformSendGiftAnchorInfo 是礼物事件中的主播信息。
type LiveOpenPlatformSendGiftAnchorInfo struct {
	UID     int64   `json:"uid"`
	OpenID  string  `json:"open_id"`
	UnionID *string `json:"union_id,omitempty"`
	UName   string  `json:"uname"`
	UFace   string  `json:"uface"`
}

// LiveOpenPlatformSendGiftComboInfo 是礼物连击信息。
type LiveOpenPlatformSendGiftComboInfo struct {
	ComboBaseNum int64  `json:"combo_base_num"`
	ComboCount   int64  `json:"combo_count"`
	ComboID      string `json:"combo_id"`
	ComboTimeout int64  `json:"combo_timeout"`
}

// LiveOpenPlatformSendGiftBlindGift 是盲盒礼物信息。
type LiveOpenPlatformSendGiftBlindGift struct {
	BlindGiftID int64 `json:"blind_gift_id"`
	Status      bool  `json:"status"`
}

// LiveOpenPlatformSuperChatEvent 是醒目留言事件。
type LiveOpenPlatformSuperChatEvent struct {
	RoomID                 int64   `json:"room_id"`
	UID                    int64   `json:"uid"`
	OpenID                 string  `json:"open_id"`
	UnionID                *string `json:"union_id,omitempty"`
	UName                  string  `json:"uname"`
	UFace                  string  `json:"uface"`
	MessageID              int64   `json:"message_id"`
	Message                string  `json:"message"`
	MsgID                  string  `json:"msg_id"`
	RMB                    float64 `json:"rmb"`
	Timestamp              int64   `json:"timestamp"`
	StartTime              int64   `json:"start_time"`
	EndTime                int64   `json:"end_time"`
	GuardLevel             int64   `json:"guard_level"`
	FansMedalLevel         int64   `json:"fans_medal_level"`
	FansMedalName          string  `json:"fans_medal_name"`
	FansMedalWearingStatus bool    `json:"fans_medal_wearing_status"`
}

// LiveOpenPlatformSuperChatDelEvent 是醒目留言撤回事件。
type LiveOpenPlatformSuperChatDelEvent struct {
	RoomID     int64   `json:"room_id"`
	MessageIDs []int64 `json:"message_ids"`
	MessageID  string  `json:"msg_id"`
}

// LiveOpenPlatformGuardEvent 是大航海事件。
type LiveOpenPlatformGuardEvent struct {
	UserInfo               *LiveOpenPlatformGuardUserInfo `json:"user_info,omitempty"`
	GuardLevel             int64                          `json:"guard_level"`
	GuardNum               int64                          `json:"guard_num"`
	GuardUnit              string                         `json:"guard_unit"`
	Price                  int64                          `json:"price"`
	FansMedalLevel         int64                          `json:"fans_medal_level"`
	FansMedalName          string                         `json:"fans_medal_name"`
	FansMedalWearingStatus bool                           `json:"fans_medal_wearing_status"`
	Timestamp              int64                          `json:"timestamp"`
	RoomID                 int64                          `json:"room_id"`
	MessageID              string                         `json:"msg_id"`
}

// LiveOpenPlatformGuardUserInfo 是大航海事件中的用户信息。
type LiveOpenPlatformGuardUserInfo struct {
	UID     int64   `json:"uid"`
	OpenID  string  `json:"open_id"`
	UnionID *string `json:"union_id,omitempty"`
	UName   string  `json:"uname"`
	UFace   string  `json:"uface"`
}

// LiveOpenPlatformLikeEvent 是点赞事件。
type LiveOpenPlatformLikeEvent struct {
	UName                  string  `json:"uname"`
	UID                    int64   `json:"uid"`
	OpenID                 string  `json:"open_id"`
	UnionID                *string `json:"union_id,omitempty"`
	UFace                  string  `json:"uface"`
	Timestamp              int64   `json:"timestamp"`
	LikeText               string  `json:"like_text"`
	LikeCount              FlexibleInt64 `json:"like_count"`
	FansMedalWearingStatus bool    `json:"fans_medal_wearing_status"`
	FansMedalName          string  `json:"fans_medal_name"`
	FansMedalLevel         int64   `json:"fans_medal_level"`
	MessageID              string  `json:"msg_id"`
	RoomID                 int64   `json:"room_id"`
}

// LiveOpenPlatformLiveRoomEnterEvent 是进入直播间事件。
type LiveOpenPlatformLiveRoomEnterEvent struct {
	UName     string  `json:"uname"`
	UID       int64   `json:"uid"`
	OpenID    string  `json:"open_id"`
	UnionID   *string `json:"union_id,omitempty"`
	UFace     string  `json:"uface"`
	Timestamp int64   `json:"timestamp"`
	RoomID    int64   `json:"room_id"`
	MessageID string  `json:"msg_id"`
}

// LiveOpenPlatformLiveStartEvent 是开播事件。
type LiveOpenPlatformLiveStartEvent struct {
	AreaName  *string `json:"area_name,omitempty"`
	OpenID    string  `json:"open_id"`
	UnionID   *string `json:"union_id,omitempty"`
	RoomID    int64   `json:"room_id"`
	Timestamp int64   `json:"timestamp"`
	Title     *string `json:"title,omitempty"`
}

// LiveOpenPlatformLiveEndEvent 是下播事件。
type LiveOpenPlatformLiveEndEvent struct {
	AreaName  *string `json:"area_name,omitempty"`
	OpenID    string  `json:"open_id"`
	UnionID   *string `json:"union_id,omitempty"`
	RoomID    int64   `json:"room_id"`
	Timestamp int64   `json:"timestamp"`
	Title     *string `json:"title,omitempty"`
}

// LiveOpenPlatformInteractionEndEvent 是互动玩法结束事件。
type LiveOpenPlatformInteractionEndEvent struct {
	GameID    string `json:"game_id"`
	Timestamp int64  `json:"timestamp"`
}
