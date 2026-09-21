package action

func GetLoginInfo() Action {
	return newAction("get_login_info", nil)
}

type GetStrangerInfoParams struct {
	UserID  int64 `json:"user_id"`
	NoCache *bool `json:"no_cache,omitempty"`
}

func GetStrangerInfo(params GetStrangerInfoParams) Action {
	return newAction("get_stranger_info", params)
}

func GetFriendList() Action {
	return newAction("get_friend_list", nil)
}

type GetGroupInfoParams struct {
	GroupID int64 `json:"group_id"`
	NoCache *bool `json:"no_cache,omitempty"`
}

func GetGroupInfo(params GetGroupInfoParams) Action {
	return newAction("get_group_info", params)
}

func GetGroupList() Action {
	return newAction("get_group_list", nil)
}

type GetGroupMemberInfoParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	NoCache *bool `json:"no_cache,omitempty"`
}

func GetGroupMemberInfo(params GetGroupMemberInfoParams) Action {
	return newAction("get_group_member_info", params)
}

type GetGroupMemberListParams struct {
	GroupID int64 `json:"group_id"`
}

func GetGroupMemberList(params GetGroupMemberListParams) Action {
	return newAction("get_group_member_list", params)
}

type GetGroupHonorInfoParams struct {
	GroupID int64  `json:"group_id"`
	Type    string `json:"type"`
}

func GetGroupHonorInfo(params GetGroupHonorInfoParams) Action {
	return newAction("get_group_honor_info", params)
}

type GetCookiesParams struct {
	Domain *string `json:"domain,omitempty"`
}

func GetCookies(params GetCookiesParams) Action {
	return newAction("get_cookies", params)
}

func GetCSRFToken() Action {
	return newAction("get_csrf_token", nil)
}

type GetCredentialsParams struct {
	Domain *string `json:"domain,omitempty"`
}

func GetCredentials(params GetCredentialsParams) Action {
	return newAction("get_credentials", params)
}
