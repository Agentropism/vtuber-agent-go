package action

type SendPrivateMsgParams struct {
	UserID     int64 `json:"user_id"`
	Message    any   `json:"message"`
	AutoEscape *bool `json:"auto_escape,omitempty"`
}

func SendPrivateMsg(params SendPrivateMsgParams) Action {
	return newAction("send_private_msg", params)
}

type SendGroupMsgParams struct {
	GroupID    int64 `json:"group_id"`
	Message    any   `json:"message"`
	AutoEscape *bool `json:"auto_escape,omitempty"`
}

func SendGroupMsg(params SendGroupMsgParams) Action {
	return newAction("send_group_msg", params)
}

type SendMsgParams struct {
	MessageType *string `json:"message_type,omitempty"`
	UserID      *int64  `json:"user_id,omitempty"`
	GroupID     *int64  `json:"group_id,omitempty"`
	Message     any     `json:"message"`
	AutoEscape  *bool   `json:"auto_escape,omitempty"`
}

func SendMsg(params SendMsgParams) Action {
	return newAction("send_msg", params)
}

type DeleteMsgParams struct {
	MessageID int64 `json:"message_id"`
}

func DeleteMsg(params DeleteMsgParams) Action {
	return newAction("delete_msg", params)
}

type GetMsgParams struct {
	MessageID int64 `json:"message_id"`
}

func GetMsg(params GetMsgParams) Action {
	return newAction("get_msg", params)
}

type GetForwardMsgParams struct {
	ID string `json:"id"`
}

func GetForwardMsg(params GetForwardMsgParams) Action {
	return newAction("get_forward_msg", params)
}
