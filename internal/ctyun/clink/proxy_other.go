//go:build !windows

package clink

import (
	"net/http"
	"net/url"
)

func clinkProxy(req *http.Request) (*url.URL, error) {
	return environmentProxy(req)
}
