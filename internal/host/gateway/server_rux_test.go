package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestServerRouterPreservesFallbackAndCORS(t *testing.T) {
	router := NewServer("", &Handler{}, NewEventBus()).router()
	cases := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "health", method: http.MethodGet, path: "/health", status: http.StatusOK},
		{name: "unknown path", method: http.MethodGet, path: "/missing", status: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPost, path: "/health", status: http.StatusMethodNotAllowed},
		{name: "preflight", method: http.MethodOptions, path: "/api/sessions", status: http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(tc.method, tc.path, nil))
			assert.Eq(t, tc.status, resp.Code)
			assert.Eq(t, "*", resp.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}
