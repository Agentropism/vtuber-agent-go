package action

func GetStatus() Action {
	return newAction("get_status", nil)
}

func GetVersionInfo() Action {
	return newAction("get_version_info", nil)
}

type SetRestartParams struct {
	Delay *int64 `json:"delay,omitempty"`
}

func SetRestart(params SetRestartParams) Action {
	return newAction("set_restart", params)
}

func CleanCache() Action {
	return newAction("clean_cache", nil)
}
