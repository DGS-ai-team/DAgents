// Package screen provides real local-desktop capture for observer streams and tools.
package screen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kbinani/screenshot"
)

// ErrUnavailable 表示当前主机无可旁观屏幕。
var ErrUnavailable = errors.New("screen_unavailable")

const (
	MaxWidth  = 1920
	MaxHeight = 1200
	MaxFPS    = 1
)

// Bounds is the OS virtual-desktop coordinate space represented by a frame.
type Bounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Frame 为一帧 JPEG 旁观画面。
type Frame struct {
	JPEG   []byte
	Width  int
	Height int
	At     time.Time
	Mime   string
	Bounds Bounds
}

// Status 描述当前屏幕能力。
type Status struct {
	Available bool   `json:"display_available"`
	Backend   string `json:"backend"`
	Reason    string `json:"reason_if_unavailable,omitempty"`
	Label     string `json:"display_label,omitempty"`
	Displays  int    `json:"display_count,omitempty"`
	Bounds    Bounds `json:"virtual_bounds,omitempty"`
}

// Capturer 可插拔采集器（Windows DXGI / macOS SCK 后续按 build tag 接入）。
type Capturer interface {
	Backend() string
	Status() Status
	Capture(ctx context.Context) (Frame, error)
}

var (
	mu       sync.RWMutex
	defaultC Capturer
)

// Default 返回进程默认采集器（惰性初始化）。
func Default() Capturer {
	mu.RLock()
	if defaultC != nil {
		c := defaultC
		mu.RUnlock()
		return c
	}
	mu.RUnlock()
	mu.Lock()
	defer mu.Unlock()
	if defaultC == nil {
		defaultC = newDefaultCapturer()
	}
	return defaultC
}

func newDefaultCapturer() Capturer {
	st := DetectStatus()
	if !st.Available {
		return &unavailableCapturer{status: st}
	}
	return &desktopCapturer{status: st}
}

// DetectStatus 与 placement localHostPayload / registrar display 启发式对齐。
func DetectStatus() Status {
	osKind := strings.ToLower(runtime.GOOS)
	label := runtime.GOOS
	backend := "desktop-capture"
	switch osKind {
	case "windows":
		label = "Windows"
		backend = "windows-gdi"
	case "darwin":
		label = "macOS"
		backend = "macos-quartz"
	case "linux", "freebsd", "openbsd", "netbsd":
		label = "Linux"
		backend = "linux-display"
		hasDisplay := strings.TrimSpace(os.Getenv("DISPLAY")) != "" ||
			strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != ""
		if !hasDisplay {
			return Status{
				Available: false,
				Backend:   "none",
				Reason:    "no_display",
				Label:     "Linux",
			}
		}
	default:
		return Status{Available: false, Backend: "none", Reason: "unsupported_os", Label: label}
	}
	bounds, displays := activeDisplayBounds()
	if displays == 0 || bounds.Empty() {
		return Status{Available: false, Backend: "none", Reason: "no_display", Label: label}
	}
	return Status{
		Available: true,
		Backend:   backend,
		Label:     label,
		Displays:  displays,
		Bounds:    boundsFromRectangle(bounds),
	}
}

type unavailableCapturer struct {
	status Status
}

func (c *unavailableCapturer) Backend() string { return c.status.Backend }
func (c *unavailableCapturer) Status() Status  { return c.status }
func (c *unavailableCapturer) Capture(context.Context) (Frame, error) {
	return Frame{}, ErrUnavailable
}

type desktopCapturer struct {
	status Status
	mu     sync.Mutex
}

func (c *desktopCapturer) Backend() string { return c.status.Backend }
func (c *desktopCapturer) Status() Status  { return c.status }

func (c *desktopCapturer) Capture(ctx context.Context) (Frame, error) {
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	bounds, displays := activeDisplayBounds()
	if displays == 0 || bounds.Empty() {
		return Frame{}, ErrUnavailable
	}
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return Frame{}, fmt.Errorf("capture desktop: %w", err)
	}
	resized := resizeWithin(img, MaxWidth, MaxHeight)
	now := time.Now().UTC()
	jpegBytes, err := encodeJPEG(resized)
	if err != nil {
		return Frame{}, err
	}
	return Frame{
		JPEG:   jpegBytes,
		Width:  resized.Bounds().Dx(),
		Height: resized.Bounds().Dy(),
		At:     now,
		Mime:   "image/jpeg",
		Bounds: boundsFromRectangle(bounds),
	}, nil
}

func activeDisplayBounds() (image.Rectangle, int) {
	displays := screenshot.NumActiveDisplays()
	if displays <= 0 {
		return image.Rectangle{}, 0
	}
	bounds := screenshot.GetDisplayBounds(0)
	for i := 1; i < displays; i++ {
		bounds = bounds.Union(screenshot.GetDisplayBounds(i))
	}
	return bounds, displays
}

func boundsFromRectangle(bounds image.Rectangle) Bounds {
	return Bounds{X: bounds.Min.X, Y: bounds.Min.Y, Width: bounds.Dx(), Height: bounds.Dy()}
}

func resizeWithin(src image.Image, maxWidth, maxHeight int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || (width <= maxWidth && height <= maxHeight) {
		return src
	}
	scaleWidth := float64(maxWidth) / float64(width)
	scaleHeight := float64(maxHeight) / float64(height)
	scale := scaleWidth
	if scaleHeight < scale {
		scale = scaleHeight
	}
	targetWidth := max(1, int(float64(width)*scale))
	targetHeight := max(1, int(float64(height)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < targetHeight; y++ {
		sy := bounds.Min.Y + min(height-1, y*height/targetHeight)
		for x := 0; x < targetWidth; x++ {
			sx := bounds.Min.X + min(width-1, x*width/targetWidth)
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}

// MinFrameInterval 对应 MaxFPS。
func MinFrameInterval() time.Duration {
	return time.Second / time.Duration(MaxFPS)
}
