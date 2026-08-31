package screen

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
)

const (
	coordinateGridLabelScale = 2
	coordinateGridLabelPad   = 4
)

// CoordinateGridStep returns the spacing used by the model-facing coordinate
// overlay. The spacing scales with the image so labels remain readable without
// covering too much of the desktop.
func CoordinateGridStep(width, height int) int {
	maxDimension := max(width, height)
	switch {
	case maxDimension >= 1600:
		return 200
	case maxDimension >= 800:
		return 100
	case maxDimension >= 400:
		return 50
	default:
		return 25
	}
}

// AnnotateCoordinates adds a lightweight pixel-coordinate grid to a JPEG.
// The returned image keeps the same dimensions as the input, which is
// important because computer_use coordinates are mapped against the original
// screenshot dimensions. The raw screenshot should remain separate for UI
// display and persistence; this image is intended for model vision input.
func AnnotateCoordinates(jpegBytes []byte) ([]byte, error) {
	if len(jpegBytes) == 0 {
		return nil, fmt.Errorf("empty screenshot")
	}
	src, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	bounds := src.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil, fmt.Errorf("invalid screenshot dimensions")
	}

	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	drawCoordinateGrid(dst, CoordinateGridStep(bounds.Dx(), bounds.Dy()))

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 84}); err != nil {
		return nil, fmt.Errorf("encode annotated screenshot: %w", err)
	}
	return out.Bytes(), nil
}

func drawCoordinateGrid(img *image.RGBA, step int) {
	if img == nil || step <= 0 {
		return
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	grid := color.RGBA{R: 43, G: 166, B: 214, A: 125}
	major := color.RGBA{R: 255, G: 185, B: 64, A: 175}
	for x := 0; x < width; x += step {
		lineColor, thickness := grid, 1
		if x%(step*5) == 0 {
			lineColor, thickness = major, 2
		}
		drawVerticalLine(img, x, thickness, lineColor)
		drawCoordinateLabel(img, fmt.Sprintf("%d", x), x+coordinateGridLabelPad, coordinateGridLabelPad)
	}
	for y := 0; y < height; y += step {
		lineColor, thickness := grid, 1
		if y%(step*5) == 0 {
			lineColor, thickness = major, 2
		}
		drawHorizontalLine(img, y, thickness, lineColor)
		drawCoordinateLabel(img, fmt.Sprintf("%d", y), coordinateGridLabelPad, y+coordinateGridLabelPad)
	}
}

func drawVerticalLine(img *image.RGBA, x, thickness int, c color.RGBA) {
	for offset := 0; offset < thickness; offset++ {
		column := x + offset
		if column >= img.Bounds().Dx() {
			continue
		}
		for y := 0; y < img.Bounds().Dy(); y++ {
			blendPixel(img, column, y, c)
		}
	}
}

func drawHorizontalLine(img *image.RGBA, y, thickness int, c color.RGBA) {
	for offset := 0; offset < thickness; offset++ {
		row := y + offset
		if row >= img.Bounds().Dy() {
			continue
		}
		for x := 0; x < img.Bounds().Dx(); x++ {
			blendPixel(img, x, row, c)
		}
	}
}

func drawCoordinateLabel(img *image.RGBA, text string, x, y int) {
	labelWidth := len(text)*6*coordinateGridLabelScale + coordinateGridLabelPad*2
	labelHeight := 7*coordinateGridLabelScale + coordinateGridLabelPad*2
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if labelWidth > width || labelHeight > height {
		return
	}
	if x+labelWidth > width {
		x = width - labelWidth
	}
	if y+labelHeight > height {
		y = height - labelHeight
	}
	fillRect(img, image.Rect(x, y, x+labelWidth, y+labelHeight), color.RGBA{R: 15, G: 23, B: 42, A: 205})
	for index := range text {
		drawDigit(img, text[index], x+coordinateGridLabelPad+index*6*coordinateGridLabelScale, y+coordinateGridLabelPad, coordinateGridLabelScale, color.RGBA{R: 255, G: 255, B: 255, A: 245})
	}
}

func drawDigit(img *image.RGBA, digit byte, x, y, scale int, c color.RGBA) {
	if digit < '0' || digit > '9' || scale <= 0 {
		return
	}
	pattern := coordinateDigitPatterns[digit-'0']
	for row, bits := range pattern {
		for column := 0; column < len(bits); column++ {
			if bits[column] != '#' {
				continue
			}
			fillRect(img, image.Rect(x+column*scale, y+row*scale, x+(column+1)*scale, y+(row+1)*scale), c)
		}
	}
}

func fillRect(img *image.RGBA, rect image.Rectangle, c color.RGBA) {
	for y := max(0, rect.Min.Y); y < min(img.Bounds().Dy(), rect.Max.Y); y++ {
		for x := max(0, rect.Min.X); x < min(img.Bounds().Dx(), rect.Max.X); x++ {
			blendPixel(img, x, y, c)
		}
	}
}

func blendPixel(img *image.RGBA, x, y int, src color.RGBA) {
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return
	}
	dst := img.RGBAAt(x, y)
	alpha := uint32(src.A)
	inverse := uint32(255 - src.A)
	img.SetRGBA(x, y, color.RGBA{
		R: uint8((uint32(src.R)*alpha + uint32(dst.R)*inverse) / 255),
		G: uint8((uint32(src.G)*alpha + uint32(dst.G)*inverse) / 255),
		B: uint8((uint32(src.B)*alpha + uint32(dst.B)*inverse) / 255),
		A: 255,
	})
}

var coordinateDigitPatterns = [10][7]string{
	{"#####", "#...#", "#...#", "#...#", "#...#", "#...#", "#####"},
	{"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", ".###."},
	{"#####", "....#", "....#", "#####", "#....", "#....", "#####"},
	{"#####", "....#", "....#", ".####", "....#", "....#", "#####"},
	{"#...#", "#...#", "#...#", "#####", "....#", "....#", "....#"},
	{"#####", "#....", "#....", "#####", "....#", "....#", "#####"},
	{"#####", "#....", "#....", "#####", "#...#", "#...#", "#####"},
	{"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	{"#####", "#...#", "#...#", "#####", "#...#", "#...#", "#####"},
	{"#####", "#...#", "#...#", "#####", "....#", "....#", "#####"},
}
