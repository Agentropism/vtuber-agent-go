# OneBot 11 Public Action Constructors

## Goal

Provide typed, convenient constructors for every public OneBot 11 action documented in `api/public.md`. Event handlers should return an action without manually creating maps or marshaling JSON.

## Scope

The implementation covers all 38 public actions in these groups:

- Message operations: send private, group, or generic messages; delete and query messages; query forwarded messages.
- Social and group administration: likes, kicks, bans, administrator and anonymous settings, cards, names, leaving groups, and special titles.
- Request handling: friend requests and group add or invitation requests.
- Information queries: login, strangers, friends, groups, members, honors, cookies, CSRF tokens, and credentials.
- Media and capability queries: records, images, and send capability checks.
- Runtime operations: status, version, restart, and cache cleanup.

Hidden and vendor-specific APIs are out of scope.

## Public API

Each action with parameters exposes a parameter struct and a constructor:

```go
action.SendPrivateMsg(action.SendPrivateMsgParams{
	UserID:  event.UserID,
	Message: event.RawMessage,
})
```

Actions without parameters expose a zero-argument constructor:

```go
action.GetLoginInfo()
```

Constructors use Go names derived directly from the OneBot action name. Parameter fields use idiomatic Go names and explicit JSON tags matching the protocol.

## Action Representation

`Action.Params` changes from `json.RawMessage` to `any`. Parameter structs remain typed, while serialization happens once when the WebSocket response is encoded. This removes constructor-time serialization failures and keeps the existing service-level error handling as the single JSON encoding boundary.

The wire representation remains unchanged:

```json
{"action":"send_private_msg","params":{"user_id":123,"message":"hello"}}
```

`Action.WithEcho(value)` returns a copy with its `echo` field set, allowing fluent correlation when required.

## Parameter Types

- User and group identifiers use `int64`.
- Message identifiers use `int64` to avoid narrowing values supplied by implementations.
- Message content uses `any`, supporting strings, CQ codes, `json.RawMessage`, and message-segment arrays.
- Protocol enum-like values remain strings so implementation-specific extensions are not blocked.
- Optional fields that must distinguish omission from a zero value use pointers with `omitempty`.
- Small helpers such as `action.Bool` and `action.Int64` create optional scalar pointers without local temporary variables.

Required parameters use value fields and are always emitted. Optional parameters with protocol defaults are omitted when their pointer is nil, allowing the OneBot implementation to apply the documented default.

## File Organization

The `internal/action` package is split by responsibility:

- `action.go`: action envelope, shared constructor, echo and optional-value helpers.
- `message.go`: message send, delete, and retrieval actions.
- `group.go`: social and group administration actions.
- `request.go`: request handling actions.
- `info.go`: account, friend, group, credential, and honor queries.
- `media.go`: record, image, and capability actions.
- `system.go`: status, version, restart, and cache actions.

The existing private-message echo handler uses `action.SendPrivateMsg` and no longer marshals parameters itself.

## Error Handling

Constructors do not return errors because they only assemble typed values. Unsupported values placed in an `any` field are reported by the existing WebSocket JSON encoding path, which logs the error and skips sending the invalid action.

## Testing

Tests verify:

- Every documented public action constructor emits the exact protocol action name.
- Representative parameter structs encode with the required JSON field names.
- Optional false and zero values can be explicitly emitted through pointer fields.
- Nil optional values are omitted so protocol defaults remain effective.
- `WithEcho` includes the correlation value.
- The private-message echo handler emits `send_private_msg` through the new constructor.

The final verification command is `go test ./...`.
