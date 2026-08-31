package screen

import (
	"context"
	"image"
	"image/color"
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

func TestResizeAndEncodeJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2400, 1600))
	img.Set(10, 10, color.RGBA{R: 255, A: 255})
	resized := resizeWithin(img, MaxWidth, MaxHeight)
	data, err := encodeJPEG(resized)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 32 {
		t.Fatalf("jpeg too small: %d", len(data))
	}
	if resized.Bounds().Dx() > MaxWidth || resized.Bounds().Dy() > MaxHeight {
		t.Fatalf("size=%dx%d", resized.Bounds().Dx(), resized.Bounds().Dy())
	}
}

func TestUnavailableCapturer(t *testing.T) {
	c := &unavailableCapturer{status: Status{Available: false, Backend: "none", Reason: "no_display"}}
	_, err := c.Capture(context.Background())
	if err != ErrUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestDefaultCapturerWhenDisplayAvailable(t *testing.T) {
	status := DetectStatus()
	if !status.Available {
		t.Skipf("display unavailable: %s", status.Reason)
	}
	frame, err := Default().Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.JPEG) < 32 || frame.Width <= 0 || frame.Height <= 0 {
		t.Fatalf("invalid frame: bytes=%d size=%dx%d", len(frame.JPEG), frame.Width, frame.Height)
	}
	if frame.Bounds.Width <= 0 || frame.Bounds.Height <= 0 {
		t.Fatalf("invalid virtual bounds: %+v", frame.Bounds)
	}
}
