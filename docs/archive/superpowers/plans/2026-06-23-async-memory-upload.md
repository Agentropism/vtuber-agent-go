# Async Memory Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upload every registered group-message and notice event without blocking dispatch or returning a OneBot action.

**Architecture:** `internal/app` keeps the existing event registrations and maps notices to `userID + text`. `internal/memory` owns a single asynchronous boundary that schedules the complete gRPC operation in a goroutine; both message and notice entry points use it, and the unary response bytes are intentionally ignored.

**Tech Stack:** Go 1.26, generic event action lists, gRPC with the existing raw JSON codec, standard `testing` package.

---

### Task 1: Add Asynchronous Upload Scheduling and Notice Mapping

**Files:**
- Create: `internal/memory/client_test.go`
- Modify: `internal/memory/client.go:58-164`

- [ ] **Step 1: Write failing tests for notice mapping and asynchronous scheduling**

```go
package memory

import (
	"testing"
	"time"
)

func TestBuildNoticePlatformEvent(t *testing.T) {
	got := buildNoticePlatformEvent(42, "QQ 42 加入了群 7")

	if got.PlatformName != "qq" {
		t.Fatalf("PlatformName = %q, want qq", got.PlatformName)
	}
	if got.UserID != "42" || got.SenderID != "42" {
		t.Fatalf("user mapping = (%q, %q), want (42, 42)", got.UserID, got.SenderID)
	}
	if got.ContentText != "QQ 42 加入了群 7" {
		t.Fatalf("ContentText = %q", got.ContentText)
	}
	if len(got.ContentData) != 1 || got.ContentData[0].Type != "text" || got.ContentData[0].Text != got.ContentText {
		t.Fatalf("ContentData = %#v", got.ContentData)
	}
	if got.MessageID != "" {
		t.Fatalf("MessageID = %q, want empty", got.MessageID)
	}
}

func TestRunAsyncDoesNotBlock(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})

	go func() {
		runAsync(func() {
			close(started)
			<-release
		})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("runAsync blocked on the upload operation")
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runAsync did not start the upload operation")
	}
	close(release)
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `go test ./internal/memory -run 'Test(BuildNoticePlatformEvent|RunAsyncDoesNotBlock)' -count=1`

Expected: build failure because `buildNoticePlatformEvent` and `runAsync` do not exist.

- [ ] **Step 3: Implement asynchronous entry points and a shared synchronous sender**

Remove `platformEventResponse`. Replace the current `Upload` body with these entry points and move its existing gRPC work into `upload`:

```go
func Upload(ctx context.Context, e event.MessageGroupEvent) {
	runAsync(func() {
		upload(ctx, buildPlatformEvent(e))
	})
}

func UploadNotice(ctx context.Context, userID int64, text string) {
	runAsync(func() {
		upload(ctx, buildNoticePlatformEvent(userID, text))
	})
}

func runAsync(fn func()) {
	go fn()
}

func upload(ctx context.Context, e platformEvent) {
	cfg, err := config.ProvideConfig()
	if err != nil {
		log.Sugar().Errorf("load memory config: %v", err)
		return
	}

	target := strings.TrimSpace(cfg.Memory.Target)
	if target == "" {
		log.Sugar().Error("memory target is empty")
		return
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawJSONCodec{})),
	)
	if err != nil {
		log.Sugar().Errorf("create memory grpc client: %v", err)
		return
	}
	defer conn.Close()

	payload, err := json.Marshal(e)
	if err != nil {
		log.Sugar().Errorf("marshal platform event: %v", err)
		return
	}

	var response []byte
	if err := conn.Invoke(ctx, "/nekro_agent.PlatformEventIngest/Ingest", payload, &response); err != nil {
		log.Sugar().Errorf("invoke memory grpc: %v", err)
		return
	}

	log.Info("memory upload done")
}
```

Add the minimal notice mapper after `buildPlatformEvent`:

```go
func buildNoticePlatformEvent(userID int64, text string) platformEvent {
	senderID := strconv.FormatInt(userID, 10)
	return platformEvent{
		PlatformName: "qq",
		UserID:       senderID,
		UserName:     senderID,
		SenderID:     senderID,
		SenderName:   senderID,
		ContentData: []platformContent{
			{Type: "text", Text: text},
		},
		ContentText: text,
	}
}
```

- [ ] **Step 4: Format and run memory tests**

Run: `gofmt -w internal/memory/client.go internal/memory/client_test.go`

Run: `go test ./internal/memory -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the memory client change**

```bash
git add internal/memory/client.go internal/memory/client_test.go
git commit -m "feat: make memory uploads asynchronous"
```

### Task 2: Connect Every Notice Handler to Upload

**Files:**
- Create: `internal/app/register_test.go`
- Modify: `internal/app/register.go:91-101`

- [ ] **Step 1: Write a failing test for notice registration behavior**

```go
package app

import (
	"context"
	"testing"

	"onebot-gateway/internal/event"
)

func TestRegisterNoticeMapsEventAndReturnsEmptyAction(t *testing.T) {
	var actions event.ActionList[int]
	mapped := false
	originalUploadNotice := uploadNotice
	t.Cleanup(func() { uploadNotice = originalUploadNotice })

	var uploadedUserID int64
	var uploadedText string
	uploadNotice = func(_ context.Context, userID int64, text string) {
		uploadedUserID = userID
		uploadedText = text
	}

	registerNotice(&actions, func(value int) noticeRecord {
		mapped = true
		return noticeRecord{userID: int64(value), text: "notice"}
	})

	got := actions.Run(42)
	if !mapped {
		t.Fatal("notice event was not mapped")
	}
	if uploadedUserID != 42 || uploadedText != "notice" {
		t.Fatalf("upload = (%d, %q), want (42, notice)", uploadedUserID, uploadedText)
	}
	if got.Action != "" || got.Params != nil || got.Echo != nil {
		t.Fatalf("action = %#v, want empty", got)
	}
}
```

- [ ] **Step 2: Run the focused app test before wiring upload**

Run: `go test ./internal/app -run TestRegisterNoticeMapsEventAndReturnsEmptyAction -count=1`

Expected: build failure because the injectable `uploadNotice` function does not exist.

- [ ] **Step 3: Upload the mapped notice through the asynchronous memory API**

Add this package variable next to the logger so the registration behavior can be tested without network access:

```go
var uploadNotice = memory.UploadNotice
```

Replace `registerNotice` with:

```go
func registerNotice[T any](actions *event.ActionList[T], record func(T) noticeRecord) {
	actions.Add(func(e T) action.Action {
		notice := record(e)
		uploadNotice(context.Background(), notice.userID, notice.text)
		return action.Action{}
	})
}
```

Keep `uploadMemory` unchanged because `memory.Upload` now owns the asynchronous boundary.

- [ ] **Step 4: Format and run app tests**

Run: `gofmt -w internal/app/register.go internal/app/register_test.go`

Run: `go test ./internal/app -count=1`

Expected: PASS. The test replaces the upload function, so it performs no network or configuration access.

- [ ] **Step 5: Commit the registration change**

```bash
git add internal/app/register.go internal/app/register_test.go
git commit -m "feat: upload registered notice events"
```

### Task 3: Repository Verification

**Files:**
- Verify only; no planned production changes.

- [ ] **Step 1: Run all tests**

Run: `go test ./... -count=1`

Expected: PASS for every package.

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: exits successfully with no diagnostics.

- [ ] **Step 3: Check formatting and worktree scope**

Run: `gofmt -l internal/app internal/memory`

Expected: no output.

Run: `git status --short`

Expected: no changes from this implementation remain uncommitted; unrelated pre-existing user changes, if any, remain untouched.
