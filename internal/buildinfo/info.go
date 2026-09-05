package buildinfo

const (
	AppName       = "CtyunHelper"
	DisplayName   = "天翼云电脑助手"
	Author        = "uvwt"
	RepositoryURL = "https://github.com/uvwt/CtyunHelper"
)

// Version 是唯一的应用版本来源。开发构建使用默认值；正式发布时可通过：
// -ldflags "-X github.com/uvwt/CtyunHelper/internal/buildinfo.Version=0.1.0"
// 注入对应版本，而无需修改 UI 代码。
var Version = "0.1.0-dev"
