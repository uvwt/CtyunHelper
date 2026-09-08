package desktop

import "fmt"

// ConnectionInfo 是 /api/desktop/client/connect 返回的 Clink 会话参数。
// Token 与 TenantMemberAccount 在轻量 REDQ 保活里不会使用，但正式 MAIN 登录
// 的 CLIENT_LOGIN_INFO(112) 会消费它们，因此必须保留在领域模型中。
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

func (c ConnectionInfo) Validate() error {
	if c.DesktopID == 0 || c.Host == "" || c.Port == "" || c.ClinkLVSOutHost == "" {
		return fmt.Errorf("desktop: connect 响应缺少 Clink 必要路由字段")
	}
	return nil
}
