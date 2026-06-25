package api

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

const usageRequestTimeout = 15 * time.Second

func newUsageHTTPClient() (*http.Client, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}

	if proxy := os.Getenv("QUOTA_TUI_PROXY"); proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid QUOTA_TUI_PROXY: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &http.Client{
		Timeout:   usageRequestTimeout,
		Transport: transport,
	}, nil
}
