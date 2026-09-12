# Async Memory Upload Design

## Goal

All registered group-message and notice handlers upload memory events without
blocking event dispatch or returning a OneBot action.

## Architecture

- Keep the existing registrations in `internal/app/register.go`.
- Every registered handler starts its upload asynchronously and immediately
  returns an empty `action.Action`.
- Group messages retain the current complete `platformEvent` mapping.
- Notice events use the existing formatted `userID` and `text` values. Fields
  unavailable from `noticeRecord`, including `message_id`, remain empty.
- The gRPC unary call still supplies a response destination because the client
  API requires one, but the response body is not decoded or returned.

## Data Flow

1. Event dispatch invokes a registered handler.
2. The handler maps the event into upload input.
3. The upload runs in a goroutine.
4. The handler immediately returns an empty `action.Action`.
5. Upload failures are logged from the background operation.

## Error Handling

Configuration, connection, serialization, and gRPC errors are logged. They do
not affect event dispatch and are not returned to OneBot.

## Verification

- Unit tests verify group-message and notice payload mapping.
- Tests verify handlers return an empty action without waiting for upload.
- `go test ./...` and `go vet ./...` verify the repository after the change.
