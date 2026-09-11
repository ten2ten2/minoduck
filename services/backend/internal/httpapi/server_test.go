package httpapi

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBindAcceptsExactlyOneJSONValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{"valid", `{"name":"MinoDuck"}`, true},
		{"valid trailing whitespace", "{\"name\":\"MinoDuck\"}\n\t", true},
		{"unknown field", `{"name":"MinoDuck","extra":true}`, false},
		{"two objects", `{"name":"MinoDuck"}{"name":"extra"}`, false},
		{"trailing scalar", `{"name":"MinoDuck"} true`, false},
		{"empty", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			var in struct {
				Name string `json:"name"`
			}
			err := bind(c, &in)
			if tc.ok {
				if err != nil || in.Name != "MinoDuck" {
					t.Fatalf("valid request rejected: value=%q error=%v", in.Name, err)
				}
				return
			}
			apiErr, ok := errors.AsType[APIError](err)
			if !ok || apiErr.Code != "INVALID_REQUEST" || apiErr.Status != 400 {
				t.Fatalf("invalid request not rejected consistently: %v", err)
			}
		})
	}
}
