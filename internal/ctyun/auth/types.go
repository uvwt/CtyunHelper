package auth

import "time"

const (
	NativeVersion     = "204010005"
	NativeAppVersion  = "4.1.0.656"
	NativeDeviceType  = "25"
	NativeAppModel    = "2"
	NativeAppChannel  = "1020400"
	NativeDeviceName  = "Windows"
	NativeDeviceModel = "windows"
	NativeSysVersion  = "Windows"
)

// ClientIdentity 描述参与天翼原生客户端 Header、签名和登录表单的终端身份。
// 正式程序固定使用 WindowsIdentity；保留显式结构是为了让协议测试可以复现官方其他端请求，
// 而不是在业务代码里散落 deviceType/version 等常量。
type ClientIdentity struct {
	Version     string
	AppVersion  string
	DeviceType  string
	AppModel    string
	AppChannel  string
	DeviceName  string
	DeviceModel string
	SysVersion  string
}

func WindowsIdentity() ClientIdentity {
	return ClientIdentity{
		Version:     NativeVersion,
		AppVersion:  NativeAppVersion,
		DeviceType:  NativeDeviceType,
		AppModel:    NativeAppModel,
		AppChannel:  NativeAppChannel,
		DeviceName:  NativeDeviceName,
		DeviceModel: NativeDeviceModel,
		SysVersion:  NativeSysVersion,
	}
}

func (i ClientIdentity) withDefaults() ClientIdentity {
	defaults := WindowsIdentity()
	if i.Version == "" {
		i.Version = defaults.Version
	}
	if i.AppVersion == "" {
		i.AppVersion = defaults.AppVersion
	}
	if i.DeviceType == "" {
		i.DeviceType = defaults.DeviceType
	}
	if i.AppModel == "" {
		i.AppModel = defaults.AppModel
	}
	if i.AppChannel == "" {
		i.AppChannel = defaults.AppChannel
	}
	if i.DeviceName == "" {
		i.DeviceName = defaults.DeviceName
	}
	if i.DeviceModel == "" {
		i.DeviceModel = defaults.DeviceModel
	}
	if i.SysVersion == "" {
		i.SysVersion = defaults.SysVersion
	}
	return i
}

type Profile struct {
	UserID               int64         `json:"userId"`
	UserEID              string        `json:"userEid"`
	TenantID             int64         `json:"tenantId"`
	SecretKey            string        `json:"secretKey"`
	CommonLoginReqHeader string        `json:"commonLoginReqHeader"`
	UserName             string        `json:"userName"`
	MobilePhone          string        `json:"mobilephone"`
	BondedDevice         bool          `json:"bondedDevice"`
	Offset               time.Duration `json:"offset"`
}

type DeviceIdentity struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
}

type RequestContext struct {
	RequestID string
	Timestamp string
	Path      string
}

// ServerData 是每个业管 Host 的节点与能力信息。serverNodeId 参与当前原生签名。
type ServerData struct {
	ServerNodeID   string `json:"serverNodeId"`
	Timestamp      int64  `json:"timestamp"`
	NetAccessType  int    `json:"netAccessType"`
	GlobalSwitches struct {
		BodyMessageEncryptionTypes []string `json:"bodyMsgEType"`
	} `json:"globalSwitches"`
}
