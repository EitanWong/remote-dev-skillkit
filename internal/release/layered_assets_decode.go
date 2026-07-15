package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

var layeredManifestFields = []string{
	"schema_version",
	"version",
	"generated_at",
	"expires_at",
	"signing_key_id",
	"assets",
	"signature",
}

var layeredAssetFields = []string{
	"id",
	"platform",
	"kind",
	"relative_path",
	"sha256",
	"size_bytes",
}

func DecodeLayeredAssetManifest(content []byte) (LayeredAssetManifest, error) {
	if err := rejectDuplicateLayeredJSONKeys(content); err != nil {
		return LayeredAssetManifest{}, fmt.Errorf("invalid layered asset manifest JSON: %w", err)
	}
	if err := validateLayeredManifestJSONFields(content); err != nil {
		return LayeredAssetManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest LayeredAssetManifest
	if err := decoder.Decode(&manifest); err != nil {
		return LayeredAssetManifest{}, fmt.Errorf("decode layered asset manifest: %w", err)
	}
	if err := requireLayeredJSONEOF(decoder); err != nil {
		return LayeredAssetManifest{}, err
	}
	return manifest, nil
}

func validateLayeredManifestJSONFields(content []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil || fields == nil {
		return fmt.Errorf("layered asset manifest must be a JSON object")
	}
	if err := requireLayeredJSONFields(fields, layeredManifestFields, nil, "layered asset manifest"); err != nil {
		return err
	}
	var assets []map[string]json.RawMessage
	if err := json.Unmarshal(fields["assets"], &assets); err != nil {
		return fmt.Errorf("layered asset manifest assets must be an array")
	}
	for _, asset := range assets {
		if asset == nil {
			return fmt.Errorf("layered asset must be a JSON object")
		}
		if err := requireLayeredJSONFields(asset, layeredAssetFields, []string{"capabilities"}, "layered asset"); err != nil {
			return err
		}
	}
	return nil
}

func requireLayeredJSONFields(fields map[string]json.RawMessage, required, optional []string, context string) error {
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, name := range required {
		allowed[name] = true
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing field in %s", context)
		}
	}
	for _, name := range optional {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return fmt.Errorf("unknown field in %s", context)
		}
	}
	return nil
}

func rejectDuplicateLayeredJSONKeys(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := walkUniqueLayeredJSONValue(decoder); err != nil {
		return err
	}
	return requireLayeredJSONEOF(decoder)
}

func walkUniqueLayeredJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON object key")
			}
			seen[key] = true
			if err := walkUniqueLayeredJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkUniqueLayeredJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
}

func requireLayeredJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("layered asset manifest contains trailing JSON")
		}
		return fmt.Errorf("decode trailing layered asset manifest content: %w", err)
	}
	return nil
}
