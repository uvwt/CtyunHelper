package clink

import (
	"net/http"
	"net/url"
	"strings"
)

func parseProxyServer(value, targetScheme string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	selected := value
	if strings.Contains(value, "=") {
		selected = ""
		want := "http"
		if targetScheme == "https" || targetScheme == "wss" {
			want = "https"
		}
		var fallback string
		for _, item := range strings.Split(value, ";") {
			name, raw, ok := strings.Cut(strings.TrimSpace(item), "=")
			if !ok {
				continue
			}
			name = strings.ToLower(strings.TrimSpace(name))
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if fallback == "" && (name == "http" || name == "https") {
				fallback = raw
			}
			if name == want {
				selected = raw
				break
			}
		}
		if selected == "" {
			selected = fallback
		}
	}
	if selected == "" {
		return nil, nil
	}
	if !strings.Contains(selected, "://") {
		selected = "http://" + selected
	}
	return url.Parse(selected)
}

func environmentProxy(req *http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment(req)
}
