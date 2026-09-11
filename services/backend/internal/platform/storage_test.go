package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type storageRoundTrip func(*http.Request) (*http.Response, error)

func (f storageRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRemoteDeleteTreatsMissingObjectAsSuccess(t *testing.T) {
	objects := Objects{
		Config: Config{
			R2Endpoint:  "https://storage.example.test",
			R2Bucket:    "private",
			R2AccessKey: "access",
			R2SecretKey: "secret",
		},
		HTTP: &http.Client{Transport: storageRoundTrip(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodDelete {
				t.Fatalf("method=%s", request.Method)
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("missing")),
				Request:    request,
			}, nil
		})},
	}
	if err := objects.Delete(context.Background(), "00000000-0000-4000-8000-000000000000/00000000-0000-4000-8000-000000000001.json"); err != nil {
		t.Fatal(err)
	}
}
