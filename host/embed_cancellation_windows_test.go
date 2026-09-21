//go:build windows

package host

import "testing"

func TestBeginWindowDestroyCancelsInFlightEmbed(t *testing.T) {
	host := &Host{hwnd: 41, embedCancellation: make(chan struct{})}
	cancelled := host.embedCancellation

	host.beginWindowDestroy(41)

	select {
	case <-cancelled:
	default:
		t.Fatal("embed cancellation remained open after window destruction")
	}
}

func TestBeginRunReplacesEmbedCancellation(t *testing.T) {
	host := New(Config{})
	old := make(chan struct{})
	close(old)
	host.embedCancellation = old

	if err := host.beginRun(); err != nil {
		t.Fatalf("beginRun: %v", err)
	}
	t.Cleanup(host.endRun)

	if host.embedCancellation == old {
		t.Fatal("beginRun reused the previous session cancellation")
	}
	select {
	case <-host.embedCancellation:
		t.Fatal("new session cancellation starts closed")
	default:
	}
}
