package api

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const usageRequestTimeout = 15 * time.Second
const defaultUsageProxy = "http://127.0.0.1:7890"

func newUsageHTTPClient() (*http.Client, error) {
	transport := &http.Transport{
		Proxy: proxyFuncForUsageRequests,
	}
	return &http.Client{
		Timeout:   usageRequestTimeout,
		Transport: transport,
	}, nil
}

func proxyFuncForUsageRequests(req *http.Request) (*url.URL, error) {
	proxy := strings.TrimSpace(os.Getenv("QUOTA_TUI_PROXY"))
	if proxy == "" {
		proxy = defaultUsageProxy
	}
	switch strings.ToLower(proxy) {
	case "direct", "none", "off", "false", "0":
		return nil, nil
	case "env":
		return http.ProxyFromEnvironment(req)
	default:
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid QUOTA_TUI_PROXY: %w", err)
		}
		return proxyURL, nil
	}
}
