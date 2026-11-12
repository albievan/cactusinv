package domain

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
)

// A picture is bounded in pixels as well as bytes (M36): a few hundred kB of PNG can
// unpack to gigabytes when it is drawn.
func TestNotePicturePixelBound(t *testing.T) {
	img := func(w, h int) string {
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
			t.Fatal(err)
		}
		return `<p><img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `"></p>`
	}
	if _, err := CleanNotes("notes", img(NoteImageMaxSide+1, 1)); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "pixels") {
		t.Fatalf("too wide: %v", err)
	}
	if _, err := CleanNotes("notes", img(4500, 4500)); !errors.Is(err, ErrValidation) {
		t.Fatalf("too many pixels: %v", err)
	}
	if _, err := CleanNotes("notes", img(1200, 800)); err != nil {
		t.Fatalf("a photo-sized picture: %v", err)
	}
}
