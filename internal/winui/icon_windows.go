//go:build windows

package winui

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"unsafe"
)

const iconResourceVersion = 0x00030000

//go:embed assets/ctyunhelper.ico
var bundledIcon []byte

var (
	createIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	destroyIconProc          = user32.NewProc("DestroyIcon")
	appIconLarge             uintptr
	appIconSmall             uintptr
)

func loadBundledAppIcons() error {
	large, err := loadBundledIcon(32, 32)
	if err != nil {
		return err
	}
	small, err := loadBundledIcon(16, 16)
	if err != nil {
		destroyIconProc.Call(large)
		return err
	}
	appIconLarge = large
	appIconSmall = small
	return nil
}

func releaseBundledAppIcons() {
	if appIconLarge != 0 {
		destroyIconProc.Call(appIconLarge)
		appIconLarge = 0
	}
	if appIconSmall != 0 {
		destroyIconProc.Call(appIconSmall)
		appIconSmall = 0
	}
}

func loadBundledIcon(width, height uint32) (uintptr, error) {
	if len(bundledIcon) < 22 || binary.LittleEndian.Uint16(bundledIcon[0:2]) != 0 || binary.LittleEndian.Uint16(bundledIcon[2:4]) != 1 {
		return 0, fmt.Errorf("内置图标格式无效")
	}
	count := int(binary.LittleEndian.Uint16(bundledIcon[4:6]))
	if count < 1 || len(bundledIcon) < 6+count*16 {
		return 0, fmt.Errorf("内置图标目录无效")
	}

	// 当前资源包含一个 256px PNG 图层，由 Windows 按标题栏/托盘需要缩放。
	entry := bundledIcon[6:22]
	size := int(binary.LittleEndian.Uint32(entry[8:12]))
	offset := int(binary.LittleEndian.Uint32(entry[12:16]))
	if size <= 0 || offset < 0 || offset+size > len(bundledIcon) {
		return 0, fmt.Errorf("内置图标数据越界")
	}
	raw := bundledIcon[offset : offset+size]
	icon, _, callErr := createIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		1,
		iconResourceVersion,
		uintptr(width),
		uintptr(height),
		0,
	)
	if icon == 0 {
		return 0, fmt.Errorf("创建内置 Windows 图标失败: %w", callErr)
	}
	return icon, nil
}
