package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_wkData_append(t *testing.T) {
	tests := []struct {
		name     string
		d        wkData
		data     map[string]interface{}
		expected wkData
	}{
		{"empty", wkData{}, wkData{"a": 1}, wkData{"a": 1}},
		{"append", wkData{"a": 1}, wkData{"b": 2}, wkData{"a": 1, "b": 2}},
		{"existing", wkData{"a": "a", "b": "b"}, wkData{"a": "aa"}, wkData{"a": "a", "b": "b"}},
		{"nested", wkData{"a": map[string]interface{}{"nest": "value"}}, wkData{"b": 2}, wkData{"b": 2, "a": map[string]interface{}{"nest": "value"}}},
		{"nestedExist", wkData{"a": map[string]interface{}{"nest": "value"}}, wkData{"a": 2}, wkData{"a": map[string]interface{}{"nest": "value"}}},
		{"nestedExistMerge", wkData{"a": map[string]interface{}{"nest": "value"}}, wkData{"a": map[string]interface{}{"nest2": "value2"}}, wkData{"a": map[string]interface{}{"nest": "value", "nest2": "value2"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.d.append(tt.data)
			assert.Equal(t, tt.expected, tt.d, "expected: %v, got: %v", tt.expected, tt.d)
		})
	}
}
