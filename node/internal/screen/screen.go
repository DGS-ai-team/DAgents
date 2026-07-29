// Package screen 提供远端旁观屏幕帧发布（只读；无键鼠）。
package screen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable 表示当前主机无可旁观屏幕。
var ErrUnavailable = errors.New("screen_unavailable")

const (
	MaxWidth  = 640
	MaxHeight = 360
	MaxFPS    = 2
)

// Frame 为一帧 JPEG 旁观画面。
type Frame struct {
	JPEG   []byte
	Width  int
	Height int
	At     time.Time
	Mime   string
}

// Status 描述当前屏幕能力。
type Status struct {
	Available bool   `json:"display_available"`
	Backend   string `json:"backend"`
	Reason    string `json:"reason_if_unavailable,omitempty"`
	Label     string `json:"display_label,omitempty"`
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
	// MVP：有 GUI 信号时用 stub 帧；真实 DXGI/SCK/X11 后续替换。
	return &stubCapturer{status: st}
}

// DetectStatus 与 placement localHostPayload / registrar display 启发式对齐。
func DetectStatus() Status {
	osKind := strings.ToLower(runtime.GOOS)
	switch osKind {
	case "windows":
		return Status{Available: true, Backend: "stub", Label: "Windows"}
	case "darwin":
		return Status{Available: true, Backend: "stub", Label: "macOS"}
	default:
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
		return Status{Available: true, Backend: "stub", Label: "Linux"}
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

type stubCapturer struct {
	status Status
	seq    int
}

func (c *stubCapturer) Backend() string { return c.status.Backend }
func (c *stubCapturer) Status() Status  { return c.status }

func (c *stubCapturer) Capture(ctx context.Context) (Frame, error) {
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}
	c.seq++
	now := time.Now().UTC()
	jpegBytes, err := renderStubJPEG(MaxWidth, MaxHeight, c.status.Label, c.seq, now)
	if err != nil {
		return Frame{}, err
	}
	return Frame{
		JPEG:   jpegBytes,
		Width:  MaxWidth,
		Height: MaxHeight,
		At:     now,
		Mime:   "image/jpeg",
	}, nil
}

func renderStubJPEG(w, h int, label string, seq int, at time.Time) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{R: 28, G: 32, B: 40, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	// 简单色带，避免纯黑；不依赖字体库。
	band := color.RGBA{R: 55, G: 148, B: 255, A: 255}
	y0 := h/2 - 12
	for y := y0; y < y0+24 && y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, band)
		}
	}
	_ = label
	_ = seq
	_ = at
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}

// MinFrameInterval 对应 MaxFPS。
func MinFrameInterval() time.Duration {
	return time.Second / time.Duration(MaxFPS)
}
