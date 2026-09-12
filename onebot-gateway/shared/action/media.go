package action

type GetRecordParams struct {
	File      string `json:"file"`
	OutFormat string `json:"out_format"`
}

func GetRecord(params GetRecordParams) Action {
	return newAction("get_record", params)
}

type GetImageParams struct {
	File string `json:"file"`
}

func GetImage(params GetImageParams) Action {
	return newAction("get_image", params)
}

func CanSendImage() Action {
	return newAction("can_send_image", nil)
}

func CanSendRecord() Action {
	return newAction("can_send_record", nil)
}
