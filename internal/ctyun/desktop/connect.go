package desktop

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// ConnectionInfo 是 /api/desktop/client/connect 返回后建立 Clink 会话所需的数据。
type ConnectionInfo struct {
	DesktopID           uint32 `json:"desktopId"`
	Host                string `json:"host"`
	Port                string `json:"port"`
	ClinkLVSOutHost     string `json:"clinkLvsOutHost"`
	CACert              string `json:"caCert"`
	ClientCert          string `json:"clientCert"`
	ClientKey           string `json:"clientKey"`
	Token               string `json:"token"`
	TenantMemberAccount string `json:"tenantMemberAccount"`
}

// SessionIdentityBuffer 对应 Clink 握手阶段的二进制会话身份结构。
// 结构由固定头（desktopId + 四组 length/offset）和四个 NUL 结尾 ASCII 字符串组成。
func (c ConnectionInfo) Validate() error {
	if c.DesktopID == 0 || c.Host == "" || c.Port == "" || c.ClinkLVSOutHost == "" || c.Token == "" || c.TenantMemberAccount == "" {
		return fmt.Errorf("desktop: connect 响应缺少 Clink 必要字段")
	}
	return nil
}

func (c ConnectionInfo) SessionIdentityBuffer(deviceCode string) ([]byte, error) {
	const (
		deviceType = "60"
		headerSize = 36
	)
	if c.Token == "" || deviceCode == "" || c.TenantMemberAccount == "" {
		return nil, fmt.Errorf("desktop: Clink 会话身份字段不完整")
	}

	values := []string{c.Token, deviceType, deviceCode, c.TenantMemberAccount}
	total := headerSize
	for _, value := range values {
		total += len(value) + 1
	}

	buf := bytes.NewBuffer(make([]byte, 0, total))
	_ = binary.Write(buf, binary.LittleEndian, c.DesktopID)
	offset := uint32(headerSize)
	for _, value := range values {
		length := uint32(len(value) + 1)
		_ = binary.Write(buf, binary.LittleEndian, length)
		_ = binary.Write(buf, binary.LittleEndian, offset)
		offset += length
	}
	for _, value := range values {
		buf.WriteString(value)
		buf.WriteByte(0)
	}
	return buf.Bytes(), nil
}
