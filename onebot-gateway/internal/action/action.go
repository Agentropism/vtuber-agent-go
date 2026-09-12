package action

type Action struct {
	Action string `json:"action"`
	Params any    `json:"params,omitempty"`
	Echo   any    `json:"echo,omitempty"`
}

func newAction(name string, params any) Action {
	return Action{Action: name, Params: params}
}

func (a Action) WithEcho(echo any) Action {
	a.Echo = echo
	return a
}
