# Event Dispatch Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove repeated event decode/dispatch blocks and event-specific default logger functions while preserving all public action lists and runtime behavior.

**Architecture:** Keep the explicit event-type switches and strongly typed exported `ActionList` variables. Add one generic helper for decode-and-dispatch and one generic constructor for action lists containing the default logging handler.

**Tech Stack:** Go 1.26, standard library generics and testing, zap logging.

---

### Task 1: Lock Handler Chain Semantics

**Files:**
- Create: `internal/event/handlers_test.go`

- [ ] **Step 1: Add a characterization test**

```go
package event

import (
	"slices"
	"testing"

	"onebot-gateway/internal/action"
)

func TestDispatchWithHandlersRunsAllAndReturnsLastNonZero(t *testing.T) {
	var calls []int
	handlers := ActionList[int]{
		func(int) action.Action {
			calls = append(calls, 1)
			return action.Action{Action: "first"}
		},
		func(int) action.Action {
			calls = append(calls, 2)
			return action.Action{}
		},
		func(int) action.Action {
			calls = append(calls, 3)
			return action.Action{Action: "last"}
		},
	}

	result := DispatchWithHandlers(42, handlers)

	if !slices.Equal(calls, []int{1, 2, 3}) {
		t.Fatalf("handler calls = %v, want [1 2 3]", calls)
	}
	if result.Action != "last" {
		t.Fatalf("result action = %q, want last", result.Action)
	}
}
```

- [ ] **Step 2: Run the characterization test**

Run: `go test ./internal/event -run TestDispatchWithHandlersRunsAllAndReturnsLastNonZero`

Expected: PASS, confirming the pre-refactor behavior.

### Task 2: Replace Repeated Default Log Handlers

**Files:**
- Modify: `internal/event/handlers.go`
- Test: `internal/event/dispatch_test.go`
- Test: `internal/event/handlers_test.go`

- [ ] **Step 1: Add the generic default-list constructor**

```go
func newActionList[T any](eventType string) ActionList[T] {
	return ActionList[T]{
		func(event T) action.Action {
			log.Sugar().Debugf("dispatch %s event: %+v", eventType, event)
			return action.Action{}
		},
	}
}
```

- [ ] **Step 2: Initialize every exported list with the constructor**

```go
var (
	MetaLifecycleActions = newActionList[MetaLifecycleEvent]("meta lifecycle")
	MetaHeartbeatActions = newActionList[MetaHeartbeatEvent]("meta heartbeat")

	MessagePrivateActions = newActionList[MessagePrivateEvent]("message private")
	MessageGroupActions   = newActionList[MessageGroupEvent]("message group")

	NoticeGroupUploadActions       = newActionList[NoticeGroupUploadEvent]("notice group_upload")
	NoticeGroupAdminActions        = newActionList[NoticeGroupAdminEvent]("notice group_admin")
	NoticeGroupDecreaseActions     = newActionList[NoticeGroupDecreaseEvent]("notice group_decrease")
	NoticeGroupIncreaseActions     = newActionList[NoticeGroupIncreaseEvent]("notice group_increase")
	NoticeGroupBanActions          = newActionList[NoticeGroupBanEvent]("notice group_ban")
	NoticeFriendAddActions         = newActionList[NoticeFriendAddEvent]("notice friend_add")
	NoticeGroupRecallActions       = newActionList[NoticeGroupRecallEvent]("notice group_recall")
	NoticeFriendRecallActions      = newActionList[NoticeFriendRecallEvent]("notice friend_recall")
	NoticeNotifyPokeActions        = newActionList[NoticeNotifyPokeEvent]("notice notify poke")
	NoticeNotifyLuckyKingActions   = newActionList[NoticeNotifyLuckyKingEvent]("notice notify lucky_king")
	NoticeNotifyHonorActions       = newActionList[NoticeNotifyHonorEvent]("notice notify honor")
	NoticeNotifyInputStatusActions = newActionList[NoticeNotifyInputStatusEvent]("notice notify input_status")

	RequestFriendActions = newActionList[RequestFriendEvent]("request friend")
	RequestGroupActions  = newActionList[RequestGroupEvent]("request group")
)
```

- [ ] **Step 3: Delete the 18 event-specific `*LogAction` functions**

Remove the functions from `metaLifecycleLogAction` through `requestGroupLogAction`; their behavior is now produced by `newActionList`.

- [ ] **Step 4: Format and run event tests**

Run: `gofmt -w internal/event/handlers.go internal/event/handlers_test.go`

Run: `go test ./internal/event`

Expected: PASS, including one unchanged log message for every supported event.

### Task 3: Extract Decode-And-Dispatch Template

**Files:**
- Modify: `internal/event/dispatch.go`
- Test: `internal/event/dispatch_test.go`

- [ ] **Step 1: Add the generic helper**

```go
func decodeAndDispatch[T any](
	baseEvent BaseEvent,
	event *T,
	eventBase *BaseEvent,
	eventType string,
	handlers ActionList[T],
) action.Action {
	if err := decodeEvent(baseEvent, event, eventBase); err != nil {
		return decodeFailed(eventType, err)
	}
	return DispatchWithHandlers(*event, handlers)
}
```

- [ ] **Step 2: Replace each repeated route body**

For each concrete event case, preserve its concrete type, diagnostic name, and typed action list. The complete replacements are:

```go
case "lifecycle":
	var event MetaLifecycleEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "meta lifecycle", MetaLifecycleActions)
case "heartbeat":
	var event MetaHeartbeatEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "meta heartbeat", MetaHeartbeatActions)

case "private":
	var event MessagePrivateEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "message private", MessagePrivateActions)
case "group":
	var event MessageGroupEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "message group", MessageGroupActions)

case "group_upload":
	var event NoticeGroupUploadEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice group_upload", NoticeGroupUploadActions)
case "group_admin":
	var event NoticeGroupAdminEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice group_admin", NoticeGroupAdminActions)
case "group_decrease":
	var event NoticeGroupDecreaseEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice group_decrease", NoticeGroupDecreaseActions)
case "group_increase":
	var event NoticeGroupIncreaseEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice group_increase", NoticeGroupIncreaseActions)
case "group_ban":
	var event NoticeGroupBanEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice group_ban", NoticeGroupBanActions)
case "friend_add":
	var event NoticeFriendAddEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice friend_add", NoticeFriendAddActions)
case "group_recall":
	var event NoticeGroupRecallEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice group_recall", NoticeGroupRecallActions)
case "friend_recall":
	var event NoticeFriendRecallEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice friend_recall", NoticeFriendRecallActions)

case "poke":
	var event NoticeNotifyPokeEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice notify poke", NoticeNotifyPokeActions)
case "lucky_king":
	var event NoticeNotifyLuckyKingEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice notify lucky_king", NoticeNotifyLuckyKingActions)
case "honor":
	var event NoticeNotifyHonorEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice notify honor", NoticeNotifyHonorActions)
case "input_status":
	var event NoticeNotifyInputStatusEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "notice notify input_status", NoticeNotifyInputStatusActions)

case "friend":
	var event RequestFriendEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "request friend", RequestFriendActions)
case "group":
	var event RequestGroupEvent
	return decodeAndDispatch(baseEvent, &event, &event.BaseEvent, "request group", RequestGroupActions)
```

- [ ] **Step 3: Format and run event tests**

Run: `gofmt -w internal/event/dispatch.go`

Run: `go test ./internal/event`

Expected: PASS.

### Task 4: Verify the Complete Repository

**Files:**
- Verify: `internal/event/dispatch.go`
- Verify: `internal/event/handlers.go`
- Verify: `internal/event/dispatch_test.go`
- Verify: `internal/event/handlers_test.go`

- [ ] **Step 1: Check formatting and whitespace**

Run: `gofmt -d internal/event/dispatch.go internal/event/handlers.go internal/event/dispatch_test.go internal/event/handlers_test.go`

Expected: no output.

Run: `git diff --check`

Expected: no output.

- [ ] **Step 2: Run all tests**

Run: `go test ./...`

Expected: all packages pass.

- [ ] **Step 3: Review the final diff**

Run: `git diff --stat && git diff -- internal/event/dispatch.go internal/event/handlers.go internal/event/dispatch_test.go internal/event/handlers_test.go`

Expected: exported action-list names remain present, route cases remain explicit, repeated logging functions are gone, and repeated decode blocks use `decodeAndDispatch`.
