//go:build rdev_bootstrap_focused

package release

import (
	"fmt"
	"strconv"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	maxFocusedLayeredAssets       = 64
	maxFocusedLayeredCapabilities = 64
)

func DecodeLayeredAssetManifest(content []byte) (LayeredAssetManifest, error) {
	parser := focusedJSONParser{content: content}
	manifest, err := parser.manifest()
	if err != nil {
		return LayeredAssetManifest{}, fmt.Errorf("decode layered asset manifest: %w", err)
	}
	parser.space()
	if parser.offset != len(parser.content) {
		return LayeredAssetManifest{}, fmt.Errorf("layered asset manifest contains trailing JSON")
	}
	return manifest, nil
}

func canonicalUnsignedLayeredAssetManifest(manifest LayeredAssetManifest) ([]byte, error) {
	canonical := cloneLayeredAssetManifest(manifest)
	canonical.Signature = ""
	for index := 1; index < len(canonical.Assets); index++ {
		for current := index; current > 0 && canonical.Assets[current].ID < canonical.Assets[current-1].ID; current-- {
			canonical.Assets[current], canonical.Assets[current-1] = canonical.Assets[current-1], canonical.Assets[current]
		}
	}
	for index := range canonical.Assets {
		values := canonical.Assets[index].Capabilities
		for item := 1; item < len(values); item++ {
			for current := item; current > 0 && values[current] < values[current-1]; current-- {
				values[current], values[current-1] = values[current-1], values[current]
			}
		}
	}

	encoded := make([]byte, 0, 512)
	encoded = append(encoded, '{')
	encoded = appendJSONField(encoded, "schema_version", canonical.SchemaVersion, false)
	encoded = appendJSONField(encoded, "version", canonical.Version, true)
	encoded = append(encoded, `,"generated_at":`...)
	var err error
	encoded, err = appendCanonicalUTC(encoded, canonical.GeneratedAt)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, `,"expires_at":`...)
	encoded, err = appendCanonicalUTC(encoded, canonical.ExpiresAt)
	if err != nil {
		return nil, err
	}
	encoded = appendJSONField(encoded, "signing_key_id", canonical.SigningKeyID, true)
	encoded = append(encoded, `,"assets":[`...)
	for index, asset := range canonical.Assets {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		encoded = append(encoded, '{')
		encoded = appendJSONField(encoded, "id", asset.ID, false)
		encoded = appendJSONField(encoded, "platform", asset.Platform, true)
		encoded = appendJSONField(encoded, "kind", asset.Kind, true)
		encoded = appendJSONField(encoded, "relative_path", asset.RelativePath, true)
		encoded = appendJSONField(encoded, "sha256", asset.SHA256, true)
		encoded = append(encoded, `,"size_bytes":`...)
		encoded = strconv.AppendInt(encoded, asset.SizeBytes, 10)
		if len(asset.Capabilities) > 0 {
			encoded = append(encoded, `,"capabilities":[`...)
			for capabilityIndex, capability := range asset.Capabilities {
				if capabilityIndex > 0 {
					encoded = append(encoded, ',')
				}
				encoded = appendJSONString(encoded, capability)
			}
			encoded = append(encoded, ']')
		}
		encoded = append(encoded, '}')
	}
	encoded = append(encoded, ']')
	encoded = appendJSONField(encoded, "signature", "", true)
	encoded = append(encoded, '}')
	return encoded, nil
}

func appendJSONField(destination []byte, name, value string, comma bool) []byte {
	if comma {
		destination = append(destination, ',')
	}
	destination = appendJSONString(destination, name)
	destination = append(destination, ':')
	return appendJSONString(destination, value)
}

func appendJSONString(destination []byte, value string) []byte {
	const hex = "0123456789abcdef"
	destination = append(destination, '"')
	for _, character := range value {
		switch character {
		case '\\', '"':
			destination = append(destination, '\\', byte(character))
		case '\b':
			destination = append(destination, `\b`...)
		case '\f':
			destination = append(destination, `\f`...)
		case '\n':
			destination = append(destination, `\n`...)
		case '\r':
			destination = append(destination, `\r`...)
		case '\t':
			destination = append(destination, `\t`...)
		default:
			if character < 0x20 || character == '<' || character == '>' || character == '&' || character == '\u2028' || character == '\u2029' {
				destination = append(destination, `\u`...)
				destination = append(destination, hex[(character>>12)&15], hex[(character>>8)&15], hex[(character>>4)&15], hex[character&15])
			} else {
				destination = utf8.AppendRune(destination, character)
			}
		}
	}
	return append(destination, '"')
}

func appendCanonicalUTC(destination []byte, value time.Time) ([]byte, error) {
	value = value.UTC()
	year, month, day := value.Date()
	if year < 0 || year > 9999 {
		return nil, fmt.Errorf("layered manifest timestamp is outside RFC3339 range")
	}
	hour, minute, second := value.Clock()
	destination = append(destination, '"')
	destination = appendFixedDecimal(destination, year, 4)
	destination = append(destination, '-')
	destination = appendFixedDecimal(destination, int(month), 2)
	destination = append(destination, '-')
	destination = appendFixedDecimal(destination, day, 2)
	destination = append(destination, 'T')
	destination = appendFixedDecimal(destination, hour, 2)
	destination = append(destination, ':')
	destination = appendFixedDecimal(destination, minute, 2)
	destination = append(destination, ':')
	destination = appendFixedDecimal(destination, second, 2)
	if nanosecond := value.Nanosecond(); nanosecond != 0 {
		fraction := [9]byte{}
		for index := len(fraction) - 1; index >= 0; index-- {
			fraction[index] = byte(nanosecond%10) + '0'
			nanosecond /= 10
		}
		end := len(fraction)
		for fraction[end-1] == '0' {
			end--
		}
		destination = append(destination, '.')
		destination = append(destination, fraction[:end]...)
	}
	return append(destination, 'Z', '"'), nil
}

func appendFixedDecimal(destination []byte, value, width int) []byte {
	start := len(destination)
	destination = append(destination, make([]byte, width)...)
	for index := start + width - 1; index >= start; index-- {
		destination[index] = byte(value%10) + '0'
		value /= 10
	}
	return destination
}

type focusedJSONParser struct {
	content []byte
	offset  int
}

func (parser *focusedJSONParser) manifest() (LayeredAssetManifest, error) {
	if err := parser.byte('{'); err != nil {
		return LayeredAssetManifest{}, err
	}
	var manifest LayeredAssetManifest
	var seen uint8
	for {
		parser.space()
		if parser.take('}') {
			break
		}
		name, err := parser.string()
		if err != nil || parser.byte(':') != nil {
			return LayeredAssetManifest{}, fmt.Errorf("invalid manifest field")
		}
		bit, err := parser.manifestField(name, &manifest)
		if err != nil || seen&bit != 0 {
			return LayeredAssetManifest{}, fmt.Errorf("invalid or duplicate manifest field %q", name)
		}
		seen |= bit
		parser.space()
		if parser.take('}') {
			break
		}
		if err := parser.byte(','); err != nil {
			return LayeredAssetManifest{}, err
		}
	}
	if seen != 0x7f {
		return LayeredAssetManifest{}, fmt.Errorf("missing manifest field")
	}
	return manifest, nil
}

func (parser *focusedJSONParser) manifestField(name string, manifest *LayeredAssetManifest) (uint8, error) {
	switch name {
	case "schema_version":
		return 1 << 0, parser.assignString(&manifest.SchemaVersion)
	case "version":
		return 1 << 1, parser.assignString(&manifest.Version)
	case "generated_at":
		return 1 << 2, parser.assignTime(&manifest.GeneratedAt)
	case "expires_at":
		return 1 << 3, parser.assignTime(&manifest.ExpiresAt)
	case "signing_key_id":
		return 1 << 4, parser.assignString(&manifest.SigningKeyID)
	case "assets":
		assets, err := parser.assets()
		manifest.Assets = assets
		return 1 << 5, err
	case "signature":
		return 1 << 6, parser.assignString(&manifest.Signature)
	default:
		return 0, fmt.Errorf("unknown manifest field")
	}
}

func (parser *focusedJSONParser) assets() ([]LayeredAsset, error) {
	if err := parser.byte('['); err != nil {
		return nil, err
	}
	var assets []LayeredAsset
	for {
		parser.space()
		if parser.take(']') {
			return assets, nil
		}
		if len(assets) >= maxFocusedLayeredAssets {
			return nil, fmt.Errorf("layered asset manifest has too many assets")
		}
		asset, err := parser.asset()
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
		parser.space()
		if parser.take(']') {
			return assets, nil
		}
		if err := parser.byte(','); err != nil {
			return nil, err
		}
	}
}

func (parser *focusedJSONParser) asset() (LayeredAsset, error) {
	if err := parser.byte('{'); err != nil {
		return LayeredAsset{}, err
	}
	var asset LayeredAsset
	var seen uint8
	for {
		name, err := parser.string()
		if err != nil || parser.byte(':') != nil {
			return LayeredAsset{}, fmt.Errorf("invalid asset field")
		}
		bit, err := parser.assetField(name, &asset)
		if err != nil || seen&bit != 0 {
			return LayeredAsset{}, fmt.Errorf("invalid or duplicate asset field %q", name)
		}
		seen |= bit
		parser.space()
		if parser.take('}') {
			break
		}
		if err := parser.byte(','); err != nil {
			return LayeredAsset{}, err
		}
	}
	if seen&0x3f != 0x3f {
		return LayeredAsset{}, fmt.Errorf("missing asset field")
	}
	return asset, nil
}

func (parser *focusedJSONParser) assetField(name string, asset *LayeredAsset) (uint8, error) {
	switch name {
	case "id":
		return 1 << 0, parser.assignString(&asset.ID)
	case "platform":
		return 1 << 1, parser.assignString(&asset.Platform)
	case "kind":
		return 1 << 2, parser.assignString(&asset.Kind)
	case "relative_path":
		return 1 << 3, parser.assignString(&asset.RelativePath)
	case "sha256":
		return 1 << 4, parser.assignString(&asset.SHA256)
	case "size_bytes":
		value, err := parser.integer()
		asset.SizeBytes = value
		return 1 << 5, err
	case "capabilities":
		values, err := parser.strings()
		asset.Capabilities = values
		return 1 << 6, err
	default:
		return 0, fmt.Errorf("unknown asset field")
	}
}

func (parser *focusedJSONParser) strings() ([]string, error) {
	if err := parser.byte('['); err != nil {
		return nil, err
	}
	var values []string
	for {
		parser.space()
		if parser.take(']') {
			return values, nil
		}
		if len(values) >= maxFocusedLayeredCapabilities {
			return nil, fmt.Errorf("layered asset has too many capabilities")
		}
		value, err := parser.string()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		parser.space()
		if parser.take(']') {
			return values, nil
		}
		if err := parser.byte(','); err != nil {
			return nil, err
		}
	}
}

func (parser *focusedJSONParser) string() (string, error) {
	parser.space()
	if !parser.take('"') {
		return "", fmt.Errorf("expected JSON string")
	}
	value := make([]byte, 0, 32)
	for parser.offset < len(parser.content) {
		character := parser.content[parser.offset]
		parser.offset++
		if character == '"' {
			if !utf8.Valid(value) {
				return "", fmt.Errorf("invalid UTF-8 in JSON string")
			}
			return string(value), nil
		}
		if character < 0x20 {
			return "", fmt.Errorf("unescaped control in JSON string")
		}
		if character != '\\' {
			value = append(value, character)
			continue
		}
		escaped, err := parser.escape()
		if err != nil {
			return "", err
		}
		value = utf8.AppendRune(value, escaped)
	}
	return "", fmt.Errorf("unterminated JSON string")
}

func (parser *focusedJSONParser) escape() (rune, error) {
	if parser.offset >= len(parser.content) {
		return 0, fmt.Errorf("unterminated JSON escape")
	}
	character := parser.content[parser.offset]
	parser.offset++
	switch character {
	case '"', '\\', '/':
		return rune(character), nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		first, err := parser.hexRune()
		if err != nil || first < 0xd800 || first > 0xdfff {
			return first, err
		}
		if first > 0xdbff || parser.offset+2 > len(parser.content) || parser.content[parser.offset] != '\\' || parser.content[parser.offset+1] != 'u' {
			return 0, fmt.Errorf("invalid JSON surrogate pair")
		}
		parser.offset += 2
		second, err := parser.hexRune()
		if err != nil || second < 0xdc00 || second > 0xdfff {
			return 0, fmt.Errorf("invalid JSON surrogate pair")
		}
		return utf16.DecodeRune(first, second), nil
	default:
		return 0, fmt.Errorf("invalid JSON escape")
	}
}

func (parser *focusedJSONParser) hexRune() (rune, error) {
	if parser.offset+4 > len(parser.content) {
		return 0, fmt.Errorf("short JSON Unicode escape")
	}
	var value rune
	for end := parser.offset + 4; parser.offset < end; parser.offset++ {
		character := parser.content[parser.offset]
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += rune(character - '0')
		case character >= 'a' && character <= 'f':
			value += rune(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += rune(character-'A') + 10
		default:
			return 0, fmt.Errorf("invalid JSON Unicode escape")
		}
	}
	return value, nil
}

func (parser *focusedJSONParser) integer() (int64, error) {
	parser.space()
	start := parser.offset
	if parser.take('-') && parser.offset >= len(parser.content) {
		return 0, fmt.Errorf("invalid JSON integer")
	}
	if parser.offset >= len(parser.content) || parser.content[parser.offset] < '0' || parser.content[parser.offset] > '9' {
		return 0, fmt.Errorf("invalid JSON integer")
	}
	if parser.content[parser.offset] == '0' {
		parser.offset++
	} else {
		for parser.offset < len(parser.content) && parser.content[parser.offset] >= '0' && parser.content[parser.offset] <= '9' {
			parser.offset++
		}
	}
	if parser.offset < len(parser.content) && (parser.content[parser.offset] == '.' || parser.content[parser.offset] == 'e' || parser.content[parser.offset] == 'E') {
		return 0, fmt.Errorf("asset size must be a JSON integer")
	}
	value, err := strconv.ParseInt(string(parser.content[start:parser.offset]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid JSON integer")
	}
	return value, nil
}

func (parser *focusedJSONParser) assignString(destination *string) error {
	value, err := parser.string()
	if err == nil {
		*destination = value
	}
	return err
}

func (parser *focusedJSONParser) assignTime(destination *time.Time) error {
	value, err := parser.string()
	if err != nil {
		return err
	}
	parsed, err := parseCanonicalUTC(value)
	if err == nil {
		*destination = parsed
	}
	return err
}

func parseCanonicalUTC(value string) (time.Time, error) {
	if len(value) < 20 || len(value) > 30 || value[4] != '-' || value[7] != '-' || value[10] != 'T' || value[13] != ':' || value[16] != ':' || value[len(value)-1] != 'Z' {
		return time.Time{}, fmt.Errorf("timestamp must use canonical UTC RFC3339")
	}
	year, ok := fixedDecimal(value[0:4])
	month, monthOK := fixedDecimal(value[5:7])
	day, dayOK := fixedDecimal(value[8:10])
	hour, hourOK := fixedDecimal(value[11:13])
	minute, minuteOK := fixedDecimal(value[14:16])
	second, secondOK := fixedDecimal(value[17:19])
	if !ok || !monthOK || !dayOK || !hourOK || !minuteOK || !secondOK {
		return time.Time{}, fmt.Errorf("timestamp contains a non-decimal component")
	}
	nanosecond := 0
	if len(value) > 20 {
		fraction := value[19 : len(value)-1]
		if len(fraction) < 2 || fraction[0] != '.' || len(fraction) > 10 || fraction[len(fraction)-1] == '0' {
			return time.Time{}, fmt.Errorf("timestamp fraction is not canonical")
		}
		fractionValue, valid := fixedDecimal(fraction[1:])
		if !valid {
			return time.Time{}, fmt.Errorf("timestamp fraction is invalid")
		}
		nanosecond = fractionValue
		for digits := len(fraction) - 1; digits < 9; digits++ {
			nanosecond *= 10
		}
	}
	parsed := time.Date(year, time.Month(month), day, hour, minute, second, nanosecond, time.UTC)
	parsedYear, parsedMonth, parsedDay := parsed.Date()
	parsedHour, parsedMinute, parsedSecond := parsed.Clock()
	if parsedYear != year || int(parsedMonth) != month || parsedDay != day || parsedHour != hour || parsedMinute != minute || parsedSecond != second || parsed.Nanosecond() != nanosecond {
		return time.Time{}, fmt.Errorf("timestamp component is outside range")
	}
	return parsed, nil
}

// IsCanonicalUTCTimestamp reports whether value is the canonical UTC form used
// by signed layered manifests and local layered-attempt state.
func IsCanonicalUTCTimestamp(value string) bool {
	_, err := parseCanonicalUTC(value)
	return err == nil
}

func fixedDecimal(value string) (int, bool) {
	result := 0
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, false
		}
		result = result*10 + int(value[index]-'0')
	}
	return result, true
}

func (parser *focusedJSONParser) byte(expected byte) error {
	parser.space()
	if !parser.take(expected) {
		return fmt.Errorf("expected JSON byte %q", expected)
	}
	return nil
}

func (parser *focusedJSONParser) take(expected byte) bool {
	if parser.offset < len(parser.content) && parser.content[parser.offset] == expected {
		parser.offset++
		return true
	}
	return false
}

func (parser *focusedJSONParser) space() {
	for parser.offset < len(parser.content) {
		switch parser.content[parser.offset] {
		case ' ', '\t', '\r', '\n':
			parser.offset++
		default:
			return
		}
	}
}
