package auth

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func testCaptchaPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 100, 50))); err != nil {
		t.Fatalf("encode captcha png: %v", err)
	}
	return buffer.Bytes()
}
