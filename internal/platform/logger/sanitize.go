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
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "api_key" || normalized == "apikey" {
		return true
	}
	for _, part := range keyParts(normalized) {
		if _, ok := sensitiveKeys[part]; ok {
			return true
		}
	}
	return false
}

func keyParts(key string) []string {
	var parts []string
	var b strings.Builder
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		if b.Len() > 0 {
			parts = append(parts, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}
