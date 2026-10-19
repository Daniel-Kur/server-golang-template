package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"
)

// ObjectSchema is deliberately limited to this template's diagnostic request.
// Extend it together with Decode, or adopt a schema library for richer APIs.
// The same schema is used for runtime validation and OpenAPI generation.
type ObjectSchema struct {
	Properties map[string]StringSchema
}
type StringSchema struct {
	MinLength int
	MaxLength int
}

// MarshalJSON fixes the types and unknown-key policy to what Decode implements.
// Callers cannot advertise unsupported types or additional properties.
func (s ObjectSchema) MarshalJSON() ([]byte, error) {
	properties := s.Properties
	if properties == nil {
		properties = map[string]StringSchema{}
	}
	return json.Marshal(struct {
		Type                 string                  `json:"type"`
		Properties           map[string]StringSchema `json:"properties"`
		AdditionalProperties bool                    `json:"additionalProperties"`
	}{Type: "object", Properties: properties})
}
func (s StringSchema) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type      string `json:"type"`
		MinLength int    `json:"minLength"`
		MaxLength int    `json:"maxLength"`
	}{Type: "string", MinLength: s.MinLength, MaxLength: s.MaxLength})
}

func (s ObjectSchema) Decode(r *http.Request) (map[string]string, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return nil, &Error{Status: 415, Code: "UNSUPPORTED_MEDIA_TYPE", Message: "Content-Type must be application/json"}
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, &Error{Status: 413, Code: "PAYLOAD_TOO_LARGE", Message: "Request body too large"}
		}
		return nil, &Error{Status: 400, Code: "BAD_REQUEST", Message: "Unable to read request body"}
	}
	invalid := &Error{Status: 422, Code: "VALIDATION_ERROR", Message: "Request validation failed"}
	if !utf8.Valid(raw) {
		return nil, invalid
	}
	// Token decoding rejects duplicate properties as well as trailing JSON values.
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid
	}
	out := make(map[string]string)
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, invalid
		}
		key, ok := token.(string)
		if !ok {
			return nil, invalid
		}
		rule, known := s.Properties[key]
		if !known {
			return nil, invalid
		}
		if _, exists := out[key]; exists {
			return nil, invalid
		}
		token, err = d.Token()
		if err != nil {
			return nil, invalid
		}
		value, ok := token.(string)
		if !ok {
			return nil, invalid
		}
		n := utf8.RuneCountInString(value)
		if n < rule.MinLength || n > rule.MaxLength {
			return nil, invalid
		}
		out[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, invalid
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, invalid
	}
	return out, nil
}
