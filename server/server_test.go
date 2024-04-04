package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeWellKnownGetter struct {
	reg *wkRegistry
}

func (f *fakeWellKnownGetter) GetData(ctx context.Context) (*wkRegistry, error) {
	return f.reg, nil
}

func Test_GetServer(t *testing.T) {
	wks := &fakeWellKnownGetter{
		reg: &wkRegistry{
			"test": {
				"key": "value",
			},
			"empty": {},
		},
	}

	tt := []struct {
		name     string
		path     string
		expected string
		code     int
	}{
		{
			name:     "existing",
			path:     "/.well-known/test",
			expected: `{"key":"value"}`,
			code:     http.StatusOK,
		},
		{
			name:     "non-existing",
			path:     "/.well-known/non-existing",
			expected: "Not found",
			code:     http.StatusNotFound,
		},
		{
			name:     "empty",
			path:     "/.well-known/empty",
			expected: "{}",
			code:     http.StatusOK,
		},
	}

	server := GetServer(wks)
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)

			if w.Code != tc.code {
				t.Errorf("Expected status code %d, got %d", tc.code, w.Code)
			}

			if w.Body.String() != tc.expected {
				t.Errorf("Expected body %s, got %s", tc.expected, w.Body.String())
			}

		})
	}
}
