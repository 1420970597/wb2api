package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

type panelRoundTrip func(*http.Request) (*http.Response, error)

func (f panelRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelsIncludesGlobalTargetCatalog(t *testing.T) {
	a := &auth.Auth{AccessToken: "at", UID: "global-1", Domain: "www.workbuddy.ai"}
	p := pool.New("")
	p.Add(a)
	p.SetCredits(a.UID, 1000, 0)

	up := upstream.New()
	up.ChatBaseGlobal = "https://fake.example"
	up.HTTP = &http.Client{Transport: panelRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":500,"msg":"unavailable"}`)),
		}, nil
	})}

	panel := New(Config{Pool: p, Upstream: up, APIKey: "test-key"})
	req := httptest.NewRequest(http.MethodGet, "/panel/api/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	panel.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Models []struct {
			ID              string   `json:"id"`
			ContextLength   int64    `json:"context_length"`
			MaxOutputTokens int64    `json:"max_output_tokens"`
			Efforts         []string `json:"supported_efforts"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 19 {
		t.Fatalf("model count=%d want 19", len(got.Models))
	}
	if got.Models[0].ID != "default-model" || got.Models[len(got.Models)-1].ID != "deepseek-v4.1-flash" {
		t.Fatalf("model order starts=%q ends=%q", got.Models[0].ID, got.Models[len(got.Models)-1].ID)
	}
	last := got.Models[len(got.Models)-1]
	if last.ContextLength != 1000000 || last.MaxOutputTokens != 128000 || len(last.Efforts) != 1 || last.Efforts[0] != "high" {
		t.Errorf("deepseek fields=%+v", last)
	}
}
