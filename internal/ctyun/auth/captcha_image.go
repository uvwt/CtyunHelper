package auth

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
)

const maxCaptchaImageBytes = 2 << 20

// readCaptchaImage 在 HTTP 边界确认验证码确实是当前客户端可解码的图片。
// 服务端异常时可能仍返回 200，但正文变成 JSON/HTML；这类响应不能继续交给 UI。
func readCaptchaImage(response *http.Response, name string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCaptchaImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("auth: 读取%s: %w", name, err)
	}
	if len(body) > maxCaptchaImageBytes {
		return nil, fmt.Errorf("auth: %s响应过大", name)
	}
	if len(body) < 16 {
		return nil, fmt.Errorf("auth: %s响应过短", name)
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		contentType := response.Header.Get("Content-Type")
		if contentType == "" {
			contentType = http.DetectContentType(body)
		}
		return nil, fmt.Errorf("auth: %s响应不是支持的图片格式（%s）: %w", name, contentType, err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
		return nil, fmt.Errorf("auth: %s尺寸异常: %dx%d", name, config.Width, config.Height)
	}
	return body, nil
}
