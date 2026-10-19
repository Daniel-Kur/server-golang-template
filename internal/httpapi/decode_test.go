package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSchemaMatchesDecoder(t *testing.T) {
	schema := ObjectSchema{Properties: map[string]StringSchema{"source": {MinLength: 1, MaxLength: 3}}}
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var documented map[string]any
	if err := json.Unmarshal(raw, &documented); err != nil {
		t.Fatal(err)
	}
	if documented["type"] != "object" || documented["additionalProperties"] != false {
		t.Fatal(documented)
	}
	property := documented["properties"].(map[string]any)["source"].(map[string]any)
	if property["type"] != "string" || property["minLength"] != float64(1) || property["maxLength"] != float64(3) {
		t.Fatal(property)
	}
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{}`, true}, {`{"source":"a"}`, true}, {`{"source":"界界界"}`, true},
		{`{"source":""}`, false}, {`{"source":"four"}`, false}, {`{"source":1}`, false},
		{`{"unknown":"a"}`, false}, {`{"source":"a","source":"b"}`, false},
		{"{\"source\":\"\xff\"}", false},
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		_, err := schema.Decode(r)
		if (err == nil) != tc.valid {
			t.Fatalf("%q: %v", tc.body, err)
		}
	}
	raw, err = json.Marshal(ObjectSchema{})
	if err != nil || !strings.Contains(string(raw), `"properties":{}`) {
		t.Fatal(string(raw), err)
	}
}
