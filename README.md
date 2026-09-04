# CtyunHelper

Windows-only 的天翼云电脑桌面助手。最终目标是单进程、纯 Go、单个 `CtyunHelper.exe`，不依赖 Docker、Python、Chromium、.NET 或 `CtYun.dll`。

## 最终目标

- 原生实现天翼账号登录、设备身份、云电脑发现与连接。
- 纯 Go 实现 Clink WebSocket 会话、REDQ 校验、保活与断线恢复，彻底替代 `CtYun.dll`。
- 纯 Go 实现积分查询、AI 对话任务与自动兑换。
- Windows 原生窗口与系统托盘，不依赖 WebView。
- 密码使用 Windows Credential Manager，敏感登录缓存使用 DPAPI。
- 内置调度与保守运行策略，不依赖 cron。

## 当前开发状态

首个开发基线已经实现：

- 天翼原生客户端基础 Header、公开签名、serverNode 签名与登录密码 challenge 算法。
- `genChallengeData -> captcha -> login` 的纯 Go HTTP 登录链，并有完整 `httptest` 请求测试。
- `getTicket` 原生签名调用。
- 积分任务、积分余额、商品、云电脑列表、兑换接口的纯 Go HTTP Client。
- Clink 状态机。
- Clink WebSocket 代理握手、初始 REDQ 帧、二进制消息编解码。
- REDQ RSA/OAEP 风格校验响应，并以独立固定测试向量校验结果。
- 103 -> 118 用户信息消息响应。
- 本地模拟 WebSocket 服务端的 Clink 全链集成测试：代理握手 -> 初始帧 -> REDQ -> 用户信息响应。
- Safety Policy：AI 2 次/天、登录 2 次/天、兑换 1 次/天、连续 3 次失败后冷却 6 小时。
- Windows GUI subsystem 主程序、单实例 Mutex、主窗口、关闭隐藏、系统托盘及打开/退出菜单。
- Windows Credential Manager 与 DPAPI 基础封装。
- 配置路径、敏感日志字段脱敏基础。

尚未完成的关键项：

- 当前官方客户端的云电脑发现 / `connect` HTTP 链还需要与真实账号做协议验证后接入。
- Clink Worker 已能完成本地协议集成测试，但**尚未在真实天翼 Clink 服务端连续在线验证**。
- EAI、自动兑换业务编排、Scheduler 和正式 UI 页面仍待迁移。

只有在不运行 `CtYun.dll` 的情况下，纯 Go 版本真实连续在线并由天翼服务端确认“使用1小时”任务完成，才视为完成对第三方保活程序的替代。

## 目录

```text
cmd/ctyun-helper       程序入口
internal/app           生命周期、事件与统一状态
internal/ctyun/auth    登录、Profile、签名与 Ticket
internal/ctyun/desktop 云电脑与 Clink 连接参数模型
internal/ctyun/clink   Clink 状态机、WebSocket、编解码与 REDQ
internal/ctyun/points  积分 HTTP 协议
internal/ctyun/eai     EAI 协议迁移边界（待实现）
internal/automation    调度与保守策略
internal/storage       配置、Credential Manager、DPAPI
internal/logging       日志脱敏与后续滚动日志
internal/winui         Windows 原生窗口、托盘与系统集成
```

## 开发验证

```bash
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags="-H=windowsgui" -o bin/CtyunHelper.exe ./cmd/ctyun-helper
```

最终 Portable 交付物只保留 `CtyunHelper.exe`；用户配置、凭据和运行状态仍按 Windows 规范存入用户配置目录、Credential Manager 和 DPAPI 保护的数据文件。
