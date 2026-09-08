package clink

import (
	"encoding/binary"
	"fmt"
)

const (
	msgMainAttach        uint16 = 104
	msgMainClientVersion uint16 = 116
	msgMainClientLogin   uint16 = 112
	msgMainLoginResponse uint16 = 136
	msgHeartbeat         uint16 = 7
)

// BuildClientLoginMessage 复现官方 clink_main_send_client_login_info 的 112 消息。
// 字节布局同时与 leleji/CtYun 历史 DesktopInfo.ToBuffer 实现一致：固定 36 字节
// 头部保存 desktopId 和四组 length/offset，随后依次写入 NUL 结尾的 ASCII 字符串。
func BuildClientLoginMessage(desktopID uint32, token, deviceCode, userAccount string) ([]byte, error) {
	const (
		headerSize = 36
		deviceType = "60"
	)
	if desktopID == 0 || token == "" || deviceCode == "" || userAccount == "" {
		return nil, fmt.Errorf("clink: 正式 MAIN 登录参数不完整")
	}
	fields := []string{token, deviceType, deviceCode, userAccount}
	total := headerSize
	for _, field := range fields {
		total += len(field) + 1
	}
	payload := make([]byte, total)
	binary.LittleEndian.PutUint32(payload[0:4], desktopID)
	offset := uint32(headerSize)
	for i, field := range fields {
		base := 4 + i*8
		length := uint32(len(field) + 1)
		binary.LittleEndian.PutUint32(payload[base:base+4], length)
		binary.LittleEndian.PutUint32(payload[base+4:base+8], offset)
		copy(payload[int(offset):int(offset)+len(field)], field)
		offset += length
	}
	return (Message{Type: msgMainClientLogin, Data: payload}).Marshal(false), nil
}

func BuildAttachChannelsMessage() []byte {
	return (Message{Type: msgMainAttach}).Marshal(false)
}

func BuildClientVersionMessage() []byte {
	return (Message{Type: msgMainClientVersion}).Marshal(false)
}

func BuildHeartbeatMessage() []byte {
	return (Message{Type: msgHeartbeat}).Marshal(false)
}

func ParseLoginResult(data []byte) (uint32, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("clink: 136 登录响应长度不足: %d", len(data))
	}
	return binary.LittleEndian.Uint32(data[:4]), nil
}
