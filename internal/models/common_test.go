package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPagination_Normalize(t *testing.T) {
	cases := []struct {
		name     string
		in       Pagination
		wantPage int
		wantSize int
	}{
		{"zero values default to 1/50", Pagination{}, 1, 50},
		{"negative page clamps to 1", Pagination{Page: -3, PageSize: 10}, 1, 10},
		{"oversize page size caps at 200", Pagination{Page: 2, PageSize: 10_000}, 2, 200},
		{"normal values pass through", Pagination{Page: 3, PageSize: 25}, 3, 25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.in
			p.Normalize()
			assert.Equal(t, tc.wantPage, p.Page)
			assert.Equal(t, tc.wantSize, p.PageSize)
		})
	}
}

func TestPagination_Offset(t *testing.T) {
	assert.Equal(t, 0, Pagination{Page: 1, PageSize: 50}.Offset())
	assert.Equal(t, 50, Pagination{Page: 2, PageSize: 50}.Offset())
	assert.Equal(t, 75, Pagination{Page: 4, PageSize: 25}.Offset())
}

func TestAPIResponse_JSONShape(t *testing.T) {
	resp := APIResponse{
		ID:           "abc",
		Status:       "OK",
		ProviderName: "goxero",
		Payload:      map[string]int{"x": 1},
	}
	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "abc", m["Id"])
	assert.Equal(t, "OK", m["Status"])
	assert.Equal(t, "goxero", m["ProviderName"])
	assert.Contains(t, m, "DateTimeUTC")
	assert.Contains(t, m, "Payload")
}

func TestErrorResponse_JSONShape(t *testing.T) {
	e := ErrorResponse{
		ErrorNumber: 400,
		Type:        "RequestError",
		Message:     "bad",
		Elements:    []ValidationItem{{Field: "email", Message: "required"}},
	}
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"ErrorNumber":400`)
	assert.Contains(t, string(raw), `"Field":"email"`)
}
