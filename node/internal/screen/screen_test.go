package screen

import (
	"context"
	"testing"
)

func TestDetectStatus_HasBackend(t *testing.T) {
	st := DetectStatus()
	if st.Backend == "" {
		t.Fatal("backend empty")
	}
	if st.Available && st.Backend == "none" {
		t.Fatalf("available with none backend: %+v", st)
	}
	if !st.Available && st.Reason == "" {
		t.Fatalf("unavailable without reason: %+v", st)
	}
}

func TestStubCapturer_ProducesJPEG(t *testing.T) {
	c := &stubCapturer{status: Status{Available: true, Backend: "stub", Label: "Test"}}
	frame, err := c.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.JPEG) < 32 {
		t.Fatalf("jpeg too small: %d", len(frame.JPEG))
	}
	if frame.Mime != "image/jpeg" {
		t.Fatalf("mime=%q", frame.Mime)
	}
	if frame.Width != MaxWidth || frame.Height != MaxHeight {
		t.Fatalf("size=%dx%d", frame.Width, frame.Height)
	}
}

func TestUnavailableCapturer(t *testing.T) {
	c := &unavailableCapturer{status: Status{Available: false, Backend: "none", Reason: "no_display"}}
	_, err := c.Capture(context.Background())
	if err != ErrUnavailable {
		t.Fatalf("err=%v", err)
	}
}
