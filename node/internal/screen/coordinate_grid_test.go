package screen

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestCoordinateGridStep(t *testing.T) {
	for _, test := range []struct {
		width, height int
		want          int
	}{
		{1920, 1080, 200},
		{1280, 720, 100},
		{640, 480, 50},
		{320, 240, 25},
	} {
		if got := CoordinateGridStep(test.width, test.height); got != test.want {
			t.Fatalf("CoordinateGridStep(%d, %d) = %d, want %d", test.width, test.height, got, test.want)
		}
	}
}

func TestDrawCoordinateGridChangesGridPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	drawCoordinateGrid(img, 100)
	if got := img.RGBAAt(100, 180); got == (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatal("vertical grid line did not change the image")
	}
	if got := img.RGBAAt(180, 100); got == (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatal("horizontal grid line did not change the image")
	}
}

func TestAnnotateCoordinatesPreservesDimensions(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < src.Bounds().Dy(); y++ {
		for x := 0; x < src.Bounds().Dx(); x++ {
			src.SetRGBA(x, y, color.RGBA{R: 32, G: 64, B: 96, A: 255})
		}
	}
	var input bytes.Buffer
	if err := jpeg.Encode(&input, src, &jpeg.Options{Quality: 82}); err != nil {
		t.Fatal(err)
	}
	annotated, err := AnnotateCoordinates(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(annotated))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 240 {
		t.Fatalf("annotated dimensions = %dx%d, want 320x240", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
	if bytes.Equal(input.Bytes(), annotated) {
		t.Fatal("annotated screenshot is identical to the raw screenshot")
	}
}
