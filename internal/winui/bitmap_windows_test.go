//go:build windows

package winui

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestDecodeCaptchaImageSupportsStandardFormats(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 8, 4))
	cases := []struct {
		name   string
		encode func(*bytes.Buffer) error
	}{
		{name: "png", encode: func(buffer *bytes.Buffer) error { return png.Encode(buffer, source) }},
		{name: "jpeg", encode: func(buffer *bytes.Buffer) error { return jpeg.Encode(buffer, source, nil) }},
		{name: "gif", encode: func(buffer *bytes.Buffer) error { return gif.Encode(buffer, source, nil) }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := testCase.encode(&buffer); err != nil {
				t.Fatalf("encode %s: %v", testCase.name, err)
			}
			decoded, err := decodeCaptchaImage(buffer.Bytes())
			if err != nil {
				t.Fatalf("decode %s: %v", testCase.name, err)
			}
			if decoded.Bounds().Dx() != 8 || decoded.Bounds().Dy() != 4 {
				t.Fatalf("decoded bounds = %v", decoded.Bounds())
			}
		})
	}
}

func TestDecodeCaptchaImageRejectsNonImage(t *testing.T) {
	_, err := decodeCaptchaImage([]byte(`{"code":500,"message":"gateway error"}`))
	if err == nil || !strings.Contains(err.Error(), "解析验证码图片") {
		t.Fatalf("expected image decode error, got %v", err)
	}
}
