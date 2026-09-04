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

当前已经实现：

- 天翼原生客户端基础 Header、公开签名、serverNode 签名与登录密码 challenge 算法。
- `genChallengeData -> captcha -> login` 的纯 Go HTTP 登录链，并有完整 `httptest` 请求测试。
- 官方原生设备绑定链：设备验证码 -> 短信验证码 -> 绑定，字段与 4.1.0.656 客户端实现保持一致。
- 账号密码保存到 Windows Credential Manager，Auth Profile 通过 DPAPI 保护后落盘；网络错误不会触发清 Profile / 重登录。
- `getTicket` 原生签名调用。
- 当前官方桌面链：`pageDesktop -> queryConnectData -> connect`，按业管 Host 获取并缓存 `serverNode`，使用区域 ServerNode 签名。
- Windows 连接参数已按官方枚举固定为 `osType=15`、`deviceId=25`，并构造连接监视器 `vdCommand`。
- 积分任务、积分余额、商品、云电脑列表、兑换接口的纯 Go HTTP Client。
- Clink 状态机。
- Clink WebSocket 代理握手、初始 REDQ 帧、二进制消息编解码。
- REDQ RSA/OAEP 风格校验响应，并以独立固定测试向量校验结果。
- 103 -> 118 用户信息消息响应。
- 本地模拟 WebSocket 服务端的 Clink 全链集成测试：代理握手 -> 初始帧 -> REDQ -> 用户信息响应。
- Safety Policy：AI 2 次/天、真实登录请求 2 次/天、兑换 1 次/天、连续 3 次失败后冷却 6 小时；额度/冷却状态持久化到用户状态目录，重启不会清零。
- EAI Gateway AES-ECB 解密、IAM Ticket SSO、RSA clientKey、AES sessionKey、Web-Signature 与 SSE Chat 全部已改成 Go 标准库实现。
- AI 积分任务编排：任务完成时不发送；真正发送前才占用每日额度；一次运行只发送一次对话，积分延迟期间只轮询状态。
- 进程内 Scheduler：AI 默认 03:00 / 20:00；积分只读刷新默认 04:00 / 06:00；兑换检查默认 04:05 / 06:05。支持手动执行、不重入、记录 LastRun/NextRun/LastError；Windows 睡眠恢复不会批量补跑错过任务。
- “使用1小时”积分任务已迁移：兑换前最多等待 80 分钟、每 5 分钟只读检查一次；超时只停止等待，不会因此重复连接云电脑。
- 自动兑换业务已迁移：默认关闭；启用后会重新验证云电脑、商品状态和积分成本，按当前通用积分计算次数，并在一次 `placeOrder` 中提交；失败后不递减数量连续试探。
- 兑换前先持久化每日 Safety 额度和 `pending` 状态；若进程在下单返回前崩溃，下次启动会把结果视为“不确定”并停止自动兑换，而不是猜测失败后再次扣积分。
- 主界面和托盘提供原生“兑换设置”窗口，可选择云电脑、商品、最大次数，以及每天/间隔天数/每月指定日期策略；保存后无需重启即可生效。
- `pending` 不确定订单必须人工核对后，在 GUI 明确选择“已确认兑换成功”或“已确认未兑换”；两种处理都保留原尝试日期，因此当天不会再次下单。
- App Keepalive 主链：认证 Profile -> 可用桌面选择 -> 连接数据解析 -> Clink Worker -> 统一 App State。
- Windows GUI subsystem 主程序、单实例 Mutex、主窗口、关闭隐藏、系统托盘；登录和设备绑定使用独立原生窗口，验证码只在内存中显示。
- 主窗口与托盘可手动执行 AI、刷新积分；启用兑换配置后还可手动“检查 / 执行兑换”。连接、积分、“使用1小时”、AI 与兑换状态都由 App Model 驱动，不直接访问协议 Client。
- 配置路径、敏感日志字段脱敏基础。

当前验证边界：

- Go `getServData` 已直接请求真实天翼服务并确认可取得 `serverNode`；服务端当前同时声明 HTTP 请求加密能力 `2/3`。
- 当前桌面/连接请求形状已与官方客户端 4.1.0.656 的实际网络日志交叉核对，并有完整 `httptest` 覆盖。
- HTTP 加密属于协商能力；当前未协商密钥时按官方客户端语义发送明文。若服务端返回 `CTG-RSPDATA-ETYPE`，程序会显式报错，不会把密文误当普通 JSON。
- 当前没有从官方客户端旧数据库页/WAL 恢复残留登录凭据做测试；真实 `pageDesktop/connect` 将通过本程序自己的正式登录流程验证。
- Clink Worker 已能完成本地协议集成测试，但**尚未在真实天翼 Clink 服务端连续在线验证**。
- Go EAI 已通过本地 Gateway -> RSA SSO -> AES sessionKey -> tenant/model -> signed SSE chat 全链测试；尚未用 CtyunHelper 自身登录态做真实 EAI 服务端验证。
- 自动兑换与积分刷新已完成本地全链单测，但**尚未使用真实账号执行 Go 版 `placeOrder`**；当前开发/测试不会触发真实兑换。
- 完整概览/任务/日志/通用设置页面仍待完善；兑换配置已经可在原生 GUI 中完成。

只有在不运行 `CtYun.dll` 的情况下，纯 Go 版本真实连续在线并由天翼服务端确认“使用1小时”任务完成，才视为完成对第三方保活程序的替代。

## 目录

```text
cmd/ctyun-helper       程序入口
internal/app           生命周期、事件与统一状态
internal/ctyun/auth    登录、Profile、签名与 Ticket
internal/ctyun/desktop 云电脑与 Clink 连接参数模型
internal/ctyun/clink   Clink 状态机、WebSocket、编解码与 REDQ
internal/ctyun/points  积分 HTTP 协议
internal/ctyun/eai     Gateway、Ticket SSO、签名与 SSE Chat
internal/automation    AI Job、Scheduler 与保守策略
internal/storage       配置、Credential Manager、DPAPI
internal/logging       日志脱敏与后续滚动日志
internal/winui         Windows 原生窗口、托盘与系统集成
```

## 自动兑换配置

自动兑换默认关闭。推荐通过主界面或托盘的“兑换设置”打开原生配置窗口：先手动刷新只读的云电脑/商品目录，再选择目标和执行策略。窗口打开本身不会联网；关闭自动兑换也不依赖网络。程序会把最终配置写入 Go `os.UserConfigDir()/CtyunHelper/config.json`（Windows 通常对应用户 AppData 配置目录）。

```json
{
  "redeem": {
    "enabled": false,
    "account": "",
    "desktopId": 0,
    "productId": 0,
    "productName": "",
    "productType": "",
    "costPoints": 0,
    "maxRedeemTimes": 0,
    "scheduleType": "daily",
    "intervalDays": 1,
    "monthlyDays": []
  }
}
```

`account` 必须填写创建这份兑换计划时的天翼账号；更换账号后，账号不匹配的兑换计划会自动禁用，不能继承到新账号。`maxRedeemTimes=0` 表示按当前积分尽量兑换；`scheduleType` 支持 `daily`、`interval_days`、`monthly_days`，其中 `monthlyDays` 的 `-1` 表示月末。兑换运行状态单独保存到用户缓存数据目录的 `redeem.json`；如果其中记录了未确认结果的 `pending`，程序会停止后续自动兑换，等待人工确认，而不会自动重试。

## 开发验证

```bash
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags="-H=windowsgui" -o bin/CtyunHelper.exe ./cmd/ctyun-helper
```

最终 Portable 交付物只保留 `CtyunHelper.exe`；用户配置、凭据和运行状态仍按 Windows 规范存入用户配置目录、Credential Manager 和 DPAPI 保护的数据文件。
