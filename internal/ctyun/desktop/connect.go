package desktop

import "fmt"

// ConnectionInfo 是 /api/desktop/client/connect 返回后建立当前 Clink 保活会话所需的数据。
// 旧 CtYun 源码模型还包含 token/tenantMemberAccount 和一套 ToBuffer，但其实际
// KeepAliveWorker 从未使用那条身份 Buffer；这里只保留当前 WebSocket 主线真实消费的字段。
type ConnectionInfo struct {
	DesktopID       uint32 `json:"desktopId"`
	Host            string `json:"host"`
	Port            string `json:"port"`
	ClinkLVSOutHost string `json:"clinkLvsOutHost"`
	CACert          string `json:"caCert"`
	ClientCert      string `json:"clientCert"`
	ClientKey       string `json:"clientKey"`
}

func (c ConnectionInfo) Validate() error {
	if c.DesktopID == 0 || c.Host == "" || c.Port == "" || c.ClinkLVSOutHost == "" {
		return fmt.Errorf("desktop: connect 响应缺少 Clink 必要路由字段")
	}
	return nil
}
