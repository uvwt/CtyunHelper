package desktop

import "strings"

// Desktop 是当前原生 pageDesktop 返回的云电脑最小业务模型。
type Desktop struct {
	ObjectType     int      `json:"objType"`
	ObjectID       string   `json:"objId"`
	ObjectName     string   `json:"objName"`
	DesktopID      string   `json:"desktopId"`
	DesktopName    string   `json:"desktopName"`
	DesktopCode    string   `json:"desktopCode"`
	UseStatus      string   `json:"useStatus"`
	UseStatusText  string   `json:"useStatusText"`
	Forbidden      bool     `json:"forbiddenConnect"`
	BackupURLs     []string `json:"backupurl"`
	ConnectionType int      `json:"connectType"`
	ConnectionAPIs APIPaths `json:"connectApiUrl"`
}

type APIPaths struct {
	ConnectPath string `json:"connectPath"`
	StatusPath  string `json:"statusPath"`
	StatePath   string `json:"statePath"`
}

func (d Desktop) ID() string {
	if strings.TrimSpace(d.DesktopID) != "" {
		return d.DesktopID
	}
	return d.ObjectID
}

func (d Desktop) Name() string {
	if d.DesktopName != "" {
		return d.DesktopName
	}
	return d.ObjectName
}

func (d Desktop) Running() bool {
	return d.UseStatusText == "运行中" || d.UseStatus == "25"
}

func (d Desktop) ConnectOrigin(fallback string) string {
	for _, value := range d.BackupURLs {
		if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
			return strings.TrimRight(value, "/")
		}
	}
	return strings.TrimRight(fallback, "/")
}
