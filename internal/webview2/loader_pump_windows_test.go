//go:build windows

package webview2

import (
	"strings"
	"testing"
	"time"
)

func TestCreationWaitCancellationWinsAfterDispatch(t *testing.T) {
	done := make(chan completion, 1)
	cancelled := make(chan struct{})
	finishCalls := 0

	_, err := waitForCreationCompletion(
		done,
		cancelled,
		time.Second,
		"the WebView2 environment",
		func() bool { return false },
		func() bool {
			close(cancelled)
			done <- completion{}
			return false
		},
		func() { finishCalls++ },
	)
	if err == nil || !strings.Contains(err.Error(), "cancelled waiting") {
		t.Fatalf("wait error = %v, want cancellation", err)
	}
	if finishCalls != 1 {
		t.Fatalf("finish calls = %d, want 1", finishCalls)
	}
}

func TestCreationWaitQuitWinsBeforeBufferedCompletion(t *testing.T) {
	done := make(chan completion, 1)
	done <- completion{}
	finishCalls := 0

	_, err := waitForCreationCompletion(
		done,
		make(chan struct{}),
		time.Second,
		"the WebView2 controller",
		func() bool { return true },
		func() bool { return false },
		func() { finishCalls++ },
	)
	if err == nil || !strings.Contains(err.Error(), "quit while waiting") {
		t.Fatalf("wait error = %v, want quit", err)
	}
	if len(done) != 1 {
		t.Fatalf("completion was consumed after quit; buffered = %d, want 1", len(done))
	}
	if finishCalls != 1 {
		t.Fatalf("finish calls = %d, want 1", finishCalls)
	}
}

func TestCreationWaitCompletionWinsWithoutTerminalCause(t *testing.T) {
	done := make(chan completion, 1)
	want := completion{hr: 17}
	done <- want

	got, err := waitForCreationCompletion(
		done,
		make(chan struct{}),
		time.Second,
		"the WebView2 environment",
		func() bool { return false },
		func() bool { return false },
		func() {},
	)
	if err != nil {
		t.Fatalf("wait error = %v", err)
	}
	if got != want {
		t.Fatalf("completion = %#v, want %#v", got, want)
	}
}
