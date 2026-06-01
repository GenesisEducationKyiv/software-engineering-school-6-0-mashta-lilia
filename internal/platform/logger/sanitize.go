package logger

import (
	"strings"
	"unicode"
)

const redactedValue = "<redacted>"

var sensitiveKeys = map[string]struct{}{
	"authorization": {},
	"card":          {},
	"cvv":           {},
	"email":         {},
	"jwt":           {},
	"password":      {},
	"secret":        {},
	"token":         {},
}

func Redact(key string, val any) any {
	if isSensitiveKey(key) {
		return redactedValue
	}
	return redactValue(val)
}

func redactValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		cp := make(map[string]any, len(v))
		for k, nested := range v {
			cp[k] = Redact(k, nested)
		}
		return cp
	case map[string]string:
		cp := make(map[string]string, len(v))
		for k, nested := range v {
			if isSensitiveKey(k) {
				cp[k] = redactedValue
				continue
			}
			cp[k] = nested
		}
		return cp
	case map[string][]string:
		cp := make(map[string][]string, len(v))
		for k, nested := range v {
			if isSensitiveKey(k) {
				cp[k] = []string{redactedValue}
				continue
			}
			cp[k] = append([]string(nil), nested...)
		}
		return cp
	case []any:
		cp := make([]any, len(v))
		for i, nested := range v {
			cp[i] = redactValue(nested)
		}
		return cp
	default:
		return val
	}
}

func isSensitiveKey(key string) bool {
	trimmed := strings.TrimSpace(key)
	normalized := strings.ToLower(trimmed)
	stripped := stripKeySeparators(normalized)
	if strings.Contains(stripped, "apikey") {
		return true
	}
	for _, part := range keyParts(trimmed) {
		if _, ok := sensitiveKeys[part]; ok {
			return true
		}
	}
	return false
}

var keySeparators = strings.NewReplacer("-", "", "_", "", ".", "", " ", "")

func stripKeySeparators(s string) string { return keySeparators.Replace(s) }

func keyParts(key string) []string {
	var parts []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			parts = append(parts, strings.ToLower(b.String()))
			b.Reset()
		}
	}
	runes := []rune(key)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				flush()
			}
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return parts
}
