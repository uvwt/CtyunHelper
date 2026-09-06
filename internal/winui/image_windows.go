//go:build windows

package winui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// decodeCaptchaImage 只解码标准库支持的图片格式，验证码始终保留在内存中。
func decodeCaptchaImage(raw []byte) (image.Image, error) {
	imageValue, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解析验证码图片: %w", err)
	}
	return imageValue, nil
}
