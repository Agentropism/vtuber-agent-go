package action

type SendLikeParams struct {
	UserID int64  `json:"user_id"`
	Times  *int64 `json:"times,omitempty"`
}

func SendLike(params SendLikeParams) Action {
	return newAction("send_like", params)
}

type SetGroupKickParams struct {
	GroupID          int64 `json:"group_id"`
	UserID           int64 `json:"user_id"`
	RejectAddRequest *bool `json:"reject_add_request,omitempty"`
}

func SetGroupKick(params SetGroupKickParams) Action {
	return newAction("set_group_kick", params)
}

type SetGroupBanParams struct {
	GroupID  int64  `json:"group_id"`
	UserID   int64  `json:"user_id"`
	Duration *int64 `json:"duration,omitempty"`
}

func SetGroupBan(params SetGroupBanParams) Action {
	return newAction("set_group_ban", params)
}

type SetGroupAnonymousBanParams struct {
	GroupID       int64  `json:"group_id"`
	Anonymous     any    `json:"anonymous,omitempty"`
	AnonymousFlag string `json:"anonymous_flag,omitempty"`
	Duration      *int64 `json:"duration,omitempty"`
}

func SetGroupAnonymousBan(params SetGroupAnonymousBanParams) Action {
	return newAction("set_group_anonymous_ban", params)
}

type SetGroupWholeBanParams struct {
	GroupID int64 `json:"group_id"`
	Enable  *bool `json:"enable,omitempty"`
}

func SetGroupWholeBan(params SetGroupWholeBanParams) Action {
	return newAction("set_group_whole_ban", params)
}

type SetGroupAdminParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	Enable  *bool `json:"enable,omitempty"`
}

func SetGroupAdmin(params SetGroupAdminParams) Action {
	return newAction("set_group_admin", params)
}

type SetGroupAnonymousParams struct {
	GroupID int64 `json:"group_id"`
	Enable  *bool `json:"enable,omitempty"`
}

func SetGroupAnonymous(params SetGroupAnonymousParams) Action {
	return newAction("set_group_anonymous", params)
}

type SetGroupCardParams struct {
	GroupID int64   `json:"group_id"`
	UserID  int64   `json:"user_id"`
	Card    *string `json:"card,omitempty"`
}

func SetGroupCard(params SetGroupCardParams) Action {
	return newAction("set_group_card", params)
}

type SetGroupNameParams struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
}

func SetGroupName(params SetGroupNameParams) Action {
	return newAction("set_group_name", params)
}

type SetGroupLeaveParams struct {
	GroupID   int64 `json:"group_id"`
	IsDismiss *bool `json:"is_dismiss,omitempty"`
}

func SetGroupLeave(params SetGroupLeaveParams) Action {
	return newAction("set_group_leave", params)
}

type SetGroupSpecialTitleParams struct {
	GroupID      int64   `json:"group_id"`
	UserID       int64   `json:"user_id"`
	SpecialTitle *string `json:"special_title,omitempty"`
	Duration     *int64  `json:"duration,omitempty"`
}

func SetGroupSpecialTitle(params SetGroupSpecialTitleParams) Action {
	return newAction("set_group_special_title", params)
}
