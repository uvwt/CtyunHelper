package desktop

type Desktop struct {
	ID   int64
	Name string
}

type ConnectData struct {
	DesktopID int64
	Address   string
	Token     string
	Node      string
	Raw       map[string]any
}
