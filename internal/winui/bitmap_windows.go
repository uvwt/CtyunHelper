//go:build windows

package winui

import (
	"bytes"
	"fmt"
	"image/png"
	"syscall"
	"unsafe"
)

const (
	biRGB        = 0
	dibRGBColors = 0
	imageBitmap  = 0
	stmSetImage  = 0x0172
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type rgbQuad struct {
	Blue     byte
	Green    byte
	Red      byte
	Reserved byte
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]rgbQuad
}

var (
	gdi32            = syscall.NewLazyDLL("gdi32.dll")
	createDIBSection = gdi32.NewProc("CreateDIBSection")
	deleteObject     = gdi32.NewProc("DeleteObject")
	sendMessageW     = user32.NewProc("SendMessageW")
)

// setPNGOnStatic 用标准库解码 PNG，再创建 32-bit top-down DIB，避免验证码落盘。
func setPNGOnStatic(control uintptr, raw []byte) (uintptr, error) {
	imageValue, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("解析验证码 PNG: %w", err)
	}
	bounds := imageValue.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || width > 2048 || height > 2048 {
		return 0, fmt.Errorf("验证码尺寸异常: %dx%d", width, height)
	}
	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(width),
		Height:      -int32(height),
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
		SizeImage:   uint32(width * height * 4),
	}}
	var bits *byte
	bitmap, _, callErr := createDIBSection.Call(
		0,
		uintptr(unsafe.Pointer(&info)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return 0, fmt.Errorf("创建验证码位图失败: %w", callErr)
	}
	pixels := unsafe.Slice(bits, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := imageValue.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			offset := (y*width + x) * 4
			pixels[offset] = byte(b >> 8)
			pixels[offset+1] = byte(g >> 8)
			pixels[offset+2] = byte(r >> 8)
			pixels[offset+3] = byte(a >> 8)
		}
	}
	old, _, _ := sendMessageW.Call(control, stmSetImage, imageBitmap, bitmap)
	if old != 0 {
		deleteObject.Call(old)
	}
	return bitmap, nil
}
