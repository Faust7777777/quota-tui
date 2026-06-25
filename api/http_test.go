package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestNewUsageHTTPClientUsesQuotaProxyEnv(t *testing.T) {
	t.Setenv("QUOTA_TUI_PROXY", "http://127.0.0.1:8888")

	client, err := newUsageHTTPClient()
	if err != nil {
		t.Fatal(err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: mustParseURL(t, "https://example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.String() != "http://127.0.0.1:8888" {
		t.Fatalf("proxy = %s, want http://127.0.0.1:8888", proxyURL)
	}
}

func TestNewUsageHTTPClientUsesLocalProxyByDefault(t *testing.T) {
	t.Setenv("QUOTA_TUI_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")

	client, err := newUsageHTTPClient()
	if err != nil {
		t.Fatal(err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: mustParseURL(t, "https://example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = %s, want http://127.0.0.1:7890", proxyURL)
	}
}

func TestNewUsageHTTPClientCanDisableProxy(t *testing.T) {
	t.Setenv("QUOTA_TUI_PROXY", "direct")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")

	client, err := newUsageHTTPClient()
	if err != nil {
		t.Fatal(err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: mustParseURL(t, "https://example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL != nil {
		t.Fatalf("proxy = %s, want nil", proxyURL)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
