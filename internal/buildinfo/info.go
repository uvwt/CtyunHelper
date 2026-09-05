package buildinfo

const (
	AppName       = "CtyunHelper"
	DisplayName   = "天翼云电脑助手"
	Author        = "uvwt"
	RepositoryURL = "https://github.com/uvwt/CtyunHelper"
)

// Version 是唯一的应用版本来源。正式发布可继续通过 -ldflags -X 覆盖，
// 便于后续版本构建时保持 UI 与发布元数据一致。
var Version = "0.1.0"
