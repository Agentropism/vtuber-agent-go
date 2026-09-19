# Event Dispatch Refactor Design

## Goal

Reduce duplication in event decoding, handler dispatch, and default logging without changing event routing or handler behavior.

## Constraints

- Preserve every exported typed action list, including its current name and event type.
- Preserve handler execution order.
- Continue returning the last non-zero `action.Action` produced by a handler.
- Preserve the default logging handler as the first handler in every action list.
- Preserve log messages, unknown-event handling, and decode-failure behavior.
- Keep the existing `switch` structure so supported event routes remain explicit and type checked.

## Design

Introduce a generic decode-and-dispatch helper. Each event route will create its concrete event value and pass it, its embedded `BaseEvent`, its diagnostic name, and its typed action list to the helper. The helper will decode the raw event, restore the original `BaseEvent`, log decode failures, and invoke `DispatchWithHandlers`.

Replace the event-specific default log functions with a generic log-handler factory. Each exported action list will initialize its first element from the factory using the same diagnostic name currently used in its log message.

`DispatchWithHandlers` and `isZeroAction` retain their existing semantics.

## Verification

- Keep the routing test covering every supported event and its exact log category.
- Add focused tests proving all handlers execute in order and the last non-zero action wins.
- Run `gofmt` and `go test ./...`.
