# OneBot 11 Public Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add typed constructors for all 38 public OneBot 11 actions so event handlers can return protocol-correct actions without manually marshaling parameters.

**Architecture:** `action.Action` stores a typed parameter value in `Params any`; JSON serialization remains at the WebSocket boundary. Public constructors are split into focused files by protocol responsibility, and optional scalar pointers preserve the difference between omitted fields and explicit zero values.

**Tech Stack:** Go 1.26, standard library `encoding/json`, table-driven `testing` tests.

---

### Task 1: Action Envelope and Shared Helpers

**Files:**
- Modify: `internal/action/action.go`
- Create: `internal/action/action_test.go`
- Modify: `internal/event/handlers.go`
- Modify: `internal/event/handlers_test.go`

- [ ] **Step 1: Write failing envelope tests**

Create `internal/action/action_test.go`:

```go
package action

import (
	"encoding/json"
	"testing"
)

func TestActionSerializesTypedParamsAndEcho(t *testing.T) {
	got, err := json.Marshal(newAction("test", struct {
		Enabled *bool `json:"enabled,omitempty"`
	}{Enabled: Bool(false)}).WithEcho(0))
	if err != nil {
		t.Fatalf("marshal action: %v", err)
	}
	const want = `{"action":"test","params":{"enabled":false},"echo":0}`
	if string(got) != want {
		t.Fatalf("action JSON = %s, want %s", got, want)
	}
}

func TestOptionalValueHelpers(t *testing.T) {
	if got := *Bool(true); !got {
		t.Fatal("Bool(true) = false")
	}
	if got := *Int64(0); got != 0 {
		t.Fatalf("Int64(0) = %d", got)
	}
	if got := *String(""); got != "" {
		t.Fatalf("String(empty) = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests and verify failure**

Run: `go test ./internal/action`

Expected: compilation fails because `newAction`, `Bool`, `Int64`, `String`, and `WithEcho` do not exist.

- [ ] **Step 3: Implement the typed envelope and helpers**

Replace `internal/action/action.go` with:

```go
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

func Bool(value bool) *bool       { return &value }
func Int64(value int64) *int64    { return &value }
func String(value string) *string { return &value }
```

Update `isZeroAction` in `internal/event/handlers.go`:

```go
func isZeroAction(next action.Action) bool {
	return next.Action == "" && next.Params == nil && next.Echo == nil
}
```

In `internal/event/handlers_test.go`, serialize `result` before decoding its parameters because `Params` is no longer a byte slice:

```go
encoded, err := json.Marshal(result)
if err != nil {
	t.Fatalf("marshal action: %v", err)
}

var got struct {
	Params map[string]any `json:"params"`
}
if err := json.Unmarshal(encoded, &got); err != nil {
	t.Fatalf("unmarshal action: %v", err)
}
params := got.Params
```

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/action ./internal/event`

Expected: PASS.

- [ ] **Step 5: Commit the envelope**

```bash
git add internal/action/action.go internal/action/action_test.go internal/event/handlers.go internal/event/handlers_test.go
git commit -m "refactor(action): support typed action params"
```

### Task 2: Message Action Constructors

**Files:**
- Create: `internal/action/message.go`
- Modify: `internal/action/action_test.go`

- [ ] **Step 1: Add failing message constructor tests**

Append a table test that checks these exact constructor/name pairs:

```go
func TestMessageActionNames(t *testing.T) {
	tests := []struct {
		name string
		got  Action
	}{
		{"send_private_msg", SendPrivateMsg(SendPrivateMsgParams{})},
		{"send_group_msg", SendGroupMsg(SendGroupMsgParams{})},
		{"send_msg", SendMsg(SendMsgParams{})},
		{"delete_msg", DeleteMsg(DeleteMsgParams{})},
		{"get_msg", GetMsg(GetMsgParams{})},
		{"get_forward_msg", GetForwardMsg(GetForwardMsgParams{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got.Action != tt.name {
				t.Fatalf("Action = %q, want %q", tt.got.Action, tt.name)
			}
		})
	}
}

func TestSendPrivateMsgJSON(t *testing.T) {
	got, err := json.Marshal(SendPrivateMsg(SendPrivateMsgParams{
		UserID:     123,
		Message:    "hello",
		AutoEscape: Bool(false),
	}))
	if err != nil {
		t.Fatalf("marshal action: %v", err)
	}
	const want = `{"action":"send_private_msg","params":{"user_id":123,"message":"hello","auto_escape":false}}`
	if string(got) != want {
		t.Fatalf("action JSON = %s, want %s", got, want)
	}
}
```

- [ ] **Step 2: Verify message tests fail**

Run: `go test ./internal/action -run 'Test(MessageActionNames|SendPrivateMsgJSON)'`

Expected: compilation fails because the message constructors are undefined.

- [ ] **Step 3: Implement all message constructors**

Create `internal/action/message.go` with these types and functions:

```go
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

func SendMsg(params SendMsgParams) Action { return newAction("send_msg", params) }

type DeleteMsgParams struct {
	MessageID int64 `json:"message_id"`
}

func DeleteMsg(params DeleteMsgParams) Action { return newAction("delete_msg", params) }

type GetMsgParams struct {
	MessageID int64 `json:"message_id"`
}

func GetMsg(params GetMsgParams) Action { return newAction("get_msg", params) }

type GetForwardMsgParams struct {
	ID string `json:"id"`
}

func GetForwardMsg(params GetForwardMsgParams) Action {
	return newAction("get_forward_msg", params)
}
```

- [ ] **Step 4: Run message tests**

Run: `go test ./internal/action -run 'Test(MessageActionNames|SendPrivateMsgJSON)'`

Expected: PASS.

- [ ] **Step 5: Commit message actions**

```bash
git add internal/action/message.go internal/action/action_test.go
git commit -m "feat(action): add message action constructors"
```

### Task 3: Group Administration and Request Constructors

**Files:**
- Create: `internal/action/group.go`
- Create: `internal/action/request.go`
- Modify: `internal/action/action_test.go`

- [ ] **Step 1: Add failing name coverage tests**

Add table entries for these constructors:

```go
tests := []struct {
	name string
	got  Action
}{
	{"send_like", SendLike(SendLikeParams{})},
	{"set_group_kick", SetGroupKick(SetGroupKickParams{})},
	{"set_group_ban", SetGroupBan(SetGroupBanParams{})},
	{"set_group_anonymous_ban", SetGroupAnonymousBan(SetGroupAnonymousBanParams{})},
	{"set_group_whole_ban", SetGroupWholeBan(SetGroupWholeBanParams{})},
	{"set_group_admin", SetGroupAdmin(SetGroupAdminParams{})},
	{"set_group_anonymous", SetGroupAnonymous(SetGroupAnonymousParams{})},
	{"set_group_card", SetGroupCard(SetGroupCardParams{})},
	{"set_group_name", SetGroupName(SetGroupNameParams{})},
	{"set_group_leave", SetGroupLeave(SetGroupLeaveParams{})},
	{"set_group_special_title", SetGroupSpecialTitle(SetGroupSpecialTitleParams{})},
	{"set_friend_add_request", SetFriendAddRequest(SetFriendAddRequestParams{})},
	{"set_group_add_request", SetGroupAddRequest(SetGroupAddRequestParams{})},
}
```

Also test explicit default overrides:

```go
func TestSetGroupRequestIncludesExplicitFalse(t *testing.T) {
	got, err := json.Marshal(SetGroupAddRequest(SetGroupAddRequestParams{
		Flag:    "request-flag",
		SubType: "add",
		Approve: Bool(false),
		Reason:  String("denied"),
	}))
	if err != nil {
		t.Fatalf("marshal action: %v", err)
	}
	const want = `{"action":"set_group_add_request","params":{"flag":"request-flag","sub_type":"add","approve":false,"reason":"denied"}}`
	if string(got) != want {
		t.Fatalf("action JSON = %s, want %s", got, want)
	}
}
```

- [ ] **Step 2: Verify group/request tests fail**

Run: `go test ./internal/action -run 'Test(Group|SetGroupRequest)'`

Expected: compilation fails because the constructors are undefined.

- [ ] **Step 3: Implement group administration constructors**

Create `internal/action/group.go` defining:

```go
package action

type SendLikeParams struct {
	UserID int64  `json:"user_id"`
	Times  *int64 `json:"times,omitempty"`
}

type SetGroupKickParams struct {
	GroupID          int64 `json:"group_id"`
	UserID           int64 `json:"user_id"`
	RejectAddRequest *bool `json:"reject_add_request,omitempty"`
}

type SetGroupBanParams struct {
	GroupID  int64  `json:"group_id"`
	UserID   int64  `json:"user_id"`
	Duration *int64 `json:"duration,omitempty"`
}

type SetGroupAnonymousBanParams struct {
	GroupID       int64  `json:"group_id"`
	Anonymous     any    `json:"anonymous,omitempty"`
	AnonymousFlag string `json:"anonymous_flag,omitempty"`
	Duration      *int64 `json:"duration,omitempty"`
}

type SetGroupWholeBanParams struct {
	GroupID int64 `json:"group_id"`
	Enable  *bool `json:"enable,omitempty"`
}

type SetGroupAdminParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	Enable  *bool `json:"enable,omitempty"`
}

type SetGroupAnonymousParams struct {
	GroupID int64 `json:"group_id"`
	Enable  *bool `json:"enable,omitempty"`
}

type SetGroupCardParams struct {
	GroupID int64   `json:"group_id"`
	UserID  int64   `json:"user_id"`
	Card    *string `json:"card,omitempty"`
}

type SetGroupNameParams struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
}

type SetGroupLeaveParams struct {
	GroupID  int64 `json:"group_id"`
	IsDismiss *bool `json:"is_dismiss,omitempty"`
}

type SetGroupSpecialTitleParams struct {
	GroupID      int64   `json:"group_id"`
	UserID       int64   `json:"user_id"`
	SpecialTitle *string `json:"special_title,omitempty"`
	Duration     *int64  `json:"duration,omitempty"`
}
```

Add direct constructors for each type using exact protocol names:

```go
func SendLike(p SendLikeParams) Action { return newAction("send_like", p) }
func SetGroupKick(p SetGroupKickParams) Action { return newAction("set_group_kick", p) }
func SetGroupBan(p SetGroupBanParams) Action { return newAction("set_group_ban", p) }
func SetGroupAnonymousBan(p SetGroupAnonymousBanParams) Action { return newAction("set_group_anonymous_ban", p) }
func SetGroupWholeBan(p SetGroupWholeBanParams) Action { return newAction("set_group_whole_ban", p) }
func SetGroupAdmin(p SetGroupAdminParams) Action { return newAction("set_group_admin", p) }
func SetGroupAnonymous(p SetGroupAnonymousParams) Action { return newAction("set_group_anonymous", p) }
func SetGroupCard(p SetGroupCardParams) Action { return newAction("set_group_card", p) }
func SetGroupName(p SetGroupNameParams) Action { return newAction("set_group_name", p) }
func SetGroupLeave(p SetGroupLeaveParams) Action { return newAction("set_group_leave", p) }
func SetGroupSpecialTitle(p SetGroupSpecialTitleParams) Action { return newAction("set_group_special_title", p) }
```

- [ ] **Step 4: Implement request constructors**

Create `internal/action/request.go`:

```go
package action

type SetFriendAddRequestParams struct {
	Flag    string  `json:"flag"`
	Approve *bool   `json:"approve,omitempty"`
	Remark  *string `json:"remark,omitempty"`
}

func SetFriendAddRequest(p SetFriendAddRequestParams) Action {
	return newAction("set_friend_add_request", p)
}

type SetGroupAddRequestParams struct {
	Flag    string  `json:"flag"`
	SubType string  `json:"sub_type"`
	Approve *bool   `json:"approve,omitempty"`
	Reason  *string `json:"reason,omitempty"`
}

func SetGroupAddRequest(p SetGroupAddRequestParams) Action {
	return newAction("set_group_add_request", p)
}
```

- [ ] **Step 5: Format and run tests**

Run: `gofmt -w internal/action/group.go internal/action/request.go internal/action/action_test.go`

Run: `go test ./internal/action`

Expected: PASS.

- [ ] **Step 6: Commit group and request actions**

```bash
git add internal/action/group.go internal/action/request.go internal/action/action_test.go
git commit -m "feat(action): add group and request constructors"
```

### Task 4: Information Query Constructors

**Files:**
- Create: `internal/action/info.go`
- Modify: `internal/action/action_test.go`

- [ ] **Step 1: Add failing query name coverage**

Add these constructor/name pairs to a table-driven test:

```go
{"get_login_info", GetLoginInfo()},
{"get_stranger_info", GetStrangerInfo(GetStrangerInfoParams{})},
{"get_friend_list", GetFriendList()},
{"get_group_info", GetGroupInfo(GetGroupInfoParams{})},
{"get_group_list", GetGroupList()},
{"get_group_member_info", GetGroupMemberInfo(GetGroupMemberInfoParams{})},
{"get_group_member_list", GetGroupMemberList(GetGroupMemberListParams{})},
{"get_group_honor_info", GetGroupHonorInfo(GetGroupHonorInfoParams{})},
{"get_cookies", GetCookies(GetCookiesParams{})},
{"get_csrf_token", GetCSRFToken()},
{"get_credentials", GetCredentials(GetCredentialsParams{})},
```

- [ ] **Step 2: Verify query tests fail**

Run: `go test ./internal/action -run TestInfoActionNames`

Expected: compilation fails because query constructors are undefined.

- [ ] **Step 3: Implement information constructors**

Create `internal/action/info.go`:

```go
package action

func GetLoginInfo() Action { return newAction("get_login_info", nil) }

type GetStrangerInfoParams struct {
	UserID  int64 `json:"user_id"`
	NoCache *bool `json:"no_cache,omitempty"`
}

func GetStrangerInfo(p GetStrangerInfoParams) Action { return newAction("get_stranger_info", p) }

func GetFriendList() Action { return newAction("get_friend_list", nil) }

type GetGroupInfoParams struct {
	GroupID int64 `json:"group_id"`
	NoCache *bool `json:"no_cache,omitempty"`
}

func GetGroupInfo(p GetGroupInfoParams) Action { return newAction("get_group_info", p) }
func GetGroupList() Action { return newAction("get_group_list", nil) }

type GetGroupMemberInfoParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	NoCache *bool `json:"no_cache,omitempty"`
}

func GetGroupMemberInfo(p GetGroupMemberInfoParams) Action {
	return newAction("get_group_member_info", p)
}

type GetGroupMemberListParams struct {
	GroupID int64 `json:"group_id"`
}

func GetGroupMemberList(p GetGroupMemberListParams) Action {
	return newAction("get_group_member_list", p)
}

type GetGroupHonorInfoParams struct {
	GroupID int64  `json:"group_id"`
	Type    string `json:"type"`
}

func GetGroupHonorInfo(p GetGroupHonorInfoParams) Action {
	return newAction("get_group_honor_info", p)
}

type GetCookiesParams struct {
	Domain *string `json:"domain,omitempty"`
}

func GetCookies(p GetCookiesParams) Action { return newAction("get_cookies", p) }
func GetCSRFToken() Action { return newAction("get_csrf_token", nil) }

type GetCredentialsParams struct {
	Domain *string `json:"domain,omitempty"`
}

func GetCredentials(p GetCredentialsParams) Action { return newAction("get_credentials", p) }
```

- [ ] **Step 4: Run information tests**

Run: `gofmt -w internal/action/info.go internal/action/action_test.go && go test ./internal/action`

Expected: PASS.

- [ ] **Step 5: Commit query actions**

```bash
git add internal/action/info.go internal/action/action_test.go
git commit -m "feat(action): add information query constructors"
```

### Task 5: Media, Capability, and Runtime Constructors

**Files:**
- Create: `internal/action/media.go`
- Create: `internal/action/system.go`
- Modify: `internal/action/action_test.go`

- [ ] **Step 1: Add failing remaining action coverage**

Add these constructor/name pairs:

```go
{"get_record", GetRecord(GetRecordParams{})},
{"get_image", GetImage(GetImageParams{})},
{"can_send_image", CanSendImage()},
{"can_send_record", CanSendRecord()},
{"get_status", GetStatus()},
{"get_version_info", GetVersionInfo()},
{"set_restart", SetRestart(SetRestartParams{})},
{"clean_cache", CleanCache()},
```

- [ ] **Step 2: Verify remaining tests fail**

Run: `go test ./internal/action -run TestRemainingActionNames`

Expected: compilation fails because media and runtime constructors are undefined.

- [ ] **Step 3: Implement media and capability constructors**

Create `internal/action/media.go`:

```go
package action

type GetRecordParams struct {
	File      string `json:"file"`
	OutFormat string `json:"out_format"`
}

func GetRecord(p GetRecordParams) Action { return newAction("get_record", p) }

type GetImageParams struct {
	File string `json:"file"`
}

func GetImage(p GetImageParams) Action { return newAction("get_image", p) }
func CanSendImage() Action { return newAction("can_send_image", nil) }
func CanSendRecord() Action { return newAction("can_send_record", nil) }
```

- [ ] **Step 4: Implement runtime constructors**

Create `internal/action/system.go`:

```go
package action

func GetStatus() Action { return newAction("get_status", nil) }
func GetVersionInfo() Action { return newAction("get_version_info", nil) }

type SetRestartParams struct {
	Delay *int64 `json:"delay,omitempty"`
}

func SetRestart(p SetRestartParams) Action { return newAction("set_restart", p) }
func CleanCache() Action { return newAction("clean_cache", nil) }
```

- [ ] **Step 5: Verify all 38 names and package tests**

Run: `gofmt -w internal/action/media.go internal/action/system.go internal/action/action_test.go`

Run: `go test ./internal/action`

Expected: PASS, with the complete name table containing 38 cases.

- [ ] **Step 6: Commit media and runtime actions**

```bash
git add internal/action/media.go internal/action/system.go internal/action/action_test.go
git commit -m "feat(action): add media and runtime constructors"
```

### Task 6: Migrate the Private Echo Handler and Verify Integration

**Files:**
- Modify: `internal/event/message_private_handlers.go`
- Modify: `internal/event/handlers_test.go`

- [ ] **Step 1: Add a failing typed-parameter assertion**

After dispatching the private message in `TestRegisterMessagePrivateEchoAction`, require the new constructor's concrete parameter type:

```go
params, ok := result.Params.(action.SendPrivateMsgParams)
if !ok {
	t.Fatalf("params type = %T, want action.SendPrivateMsgParams", result.Params)
}
if params.UserID != 123 || params.Message != "hello" {
	t.Fatalf("params = %+v", params)
}
```

- [ ] **Step 2: Run the event test before migration**

Run: `go test ./internal/event -run TestRegisterMessagePrivateEchoAction`

Expected: FAIL because the old handler stores `json.RawMessage` rather than `action.SendPrivateMsgParams`.

- [ ] **Step 3: Migrate the echo handler**

Remove `encoding/json` from `internal/event/message_private_handlers.go` and implement the registered handler as:

```go
func RegisterMessagePrivateEchoAction() {
	RegisterMessagePrivateAction(func(event MessagePrivateEvent) action.Action {
		return action.SendPrivateMsg(action.SendPrivateMsgParams{
			UserID:  event.UserID,
			Message: event.RawMessage,
		})
	})
}
```

- [ ] **Step 4: Run event tests**

Run: `gofmt -w internal/event/message_private_handlers.go internal/event/handlers_test.go`

Run: `go test ./internal/event`

Expected: PASS.

- [ ] **Step 5: Run full verification**

Run: `go test ./...`

Expected: every package passes.

Run: `git diff --check`

Expected: no whitespace errors.

- [ ] **Step 6: Commit integration changes**

```bash
git add internal/event/message_private_handlers.go internal/event/handlers_test.go
git commit -m "refactor(event): use private message action constructor"
```
