package repository

import "encoding/json"

// jsonUnmarshal and mappingJSON are the two directions of the JSONB round-trip
// used by the repositories that keep a free-form body column (bank rule
// definitions, statement import mappings). They mirror the handlers' helpers
// (jsonUnmarshalBytes / mustJSON) but live here so repositories need not import
// the handlers package.
//
// Fallback convention per column: the mapping column is a JSON object, so a
// missing or unencodable value falls back to "{}"; array payloads use "[]"
// (handlers.mustJSON). Keeping each fallback to the column's shape means a
// malformed value can never write the wrong JSON type into jsonb.

func jsonUnmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// mappingJSON encodes the free-form column mapping for the mapping jsonb
// column, defaulting to the empty object the column itself defaults to.
func mappingJSON(m map[string]any) []byte {
	if m == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}
