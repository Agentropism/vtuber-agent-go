package action

type SetFriendAddRequestParams struct {
	Flag    string  `json:"flag"`
	Approve *bool   `json:"approve,omitempty"`
	Remark  *string `json:"remark,omitempty"`
}

func SetFriendAddRequest(params SetFriendAddRequestParams) Action {
	return newAction("set_friend_add_request", params)
}

type SetGroupAddRequestParams struct {
	Flag    string  `json:"flag"`
	SubType string  `json:"sub_type"`
	Approve *bool   `json:"approve,omitempty"`
	Reason  *string `json:"reason,omitempty"`
}

func SetGroupAddRequest(params SetGroupAddRequestParams) Action {
	return newAction("set_group_add_request", params)
}
