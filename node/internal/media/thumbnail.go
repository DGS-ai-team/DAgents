package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"strings"
)

const ThumbnailMaxEdge = 480

// ThumbnailFromFile 读取图片并在最长边超过 ThumbnailMaxEdge 时生成缩略图。
// served 为 false 时调用方应回退为原图直出。
func ThumbnailFromFile(path, mime string) (data []byte, contentType string, served bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", false, err
	}
	defer f.Close()
	return ThumbnailFromReader(f, mime)
}

// ThumbnailFromReader 从 reader 解码并生成缩略图（F-M6）。
func ThumbnailFromReader(r io.Reader, mime string) (data []byte, contentType string, served bool, err error) {
	img, err := decodeImage(r, mime)
	if err != nil {
		return nil, "", false, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, "", false, fmt.Errorf("invalid image dimensions")
	}
	maxEdge := w
	if h > maxEdge {
		maxEdge = h
	}
	if maxEdge <= ThumbnailMaxEdge {
		return nil, "", false, nil
	}
	scale := float64(ThumbnailMaxEdge) / float64(maxEdge)
	nw := max(1, int(float64(w)*scale+0.5))
	nh := max(1, int(float64(h)*scale+0.5))
	dst := resizeNearest(img, nw, nh)
	outMIME := outputMIME(mime)
	data, err = encodeImage(dst, outMIME)
	if err != nil {
		return nil, "", false, err
	}
	return data, outMIME, true, nil
}

func decodeImage(r io.Reader, mime string) (image.Image, error) {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg":
		return jpeg.Decode(r)
	case "image/png":
		return png.Decode(r)
	case "image/gif":
		return gif.Decode(r)
	default:
		return nil, fmt.Errorf("unsupported image mime for thumbnail: %s", mime)
	}
}

func resizeNearest(src image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	srcBounds := src.Bounds()
	for y := 0; y < height; y++ {
		sy := srcBounds.Min.Y + (y*srcBounds.Dy())/height
		for x := 0; x < width; x++ {
			sx := srcBounds.Min.X + (x*srcBounds.Dx())/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func outputMIME(sourceMIME string) string {
	if strings.EqualFold(strings.TrimSpace(sourceMIME), "image/png") {
		return "image/png"
	}
	return "image/jpeg"
}

func encodeImage(img image.Image, mime string) ([]byte, error) {
	var buf bytes.Buffer
	switch mime {
	case "image/png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
	default:
		rgba := image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
		if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// solidColorImage 供单测构造有效 PNG。
func solidColorImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}
