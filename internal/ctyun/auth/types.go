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

type Profile struct {
	UserID               int64
	UserEID              string
	TenantID             int64
	SecretKey            string
	CommonLoginReqHeader string
	UserName             string
	MobilePhone          string
	BondedDevice         bool
	Offset               time.Duration
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
