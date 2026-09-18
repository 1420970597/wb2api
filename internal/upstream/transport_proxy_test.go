package upstream

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSetProxyURL(t *testing.T) {
	c := New()
	if err := c.SetProxyURL("http://user:pass@proxy.example:8080"); err != nil {
		t.Fatalf("SetProxyURL: %v", err)
	}
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatal("HTTP transport type")
	}
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "upstream.example"}}
	got, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("proxy func: %v", err)
	}
	if got == nil || got.String() != "http://user:pass@proxy.example:8080" {
		t.Fatalf("proxy=%v", got)
	}
	if err := c.SetProxyURL(""); err != nil {
		t.Fatalf("clear proxy: %v", err)
	}
	if tr.Proxy != nil {
		t.Fatal("proxy should be cleared")
	}
}

func TestSetProxyURLRejectsInvalidURL(t *testing.T) {
	if err := New().SetProxyURL("socks5://proxy.example:1080"); err == nil {
		t.Fatal("want invalid proxy URL error")
	}
}
