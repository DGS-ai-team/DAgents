package workgroup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// CanonicalJSON 按 D0.5 §1.4：键字典序、紧凑 UTF-8、排除 digest/payload_hash。
func CanonicalJSON(v any) ([]byte, error) {
	normalized, err := canonicalize(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

// SHA256Digest 返回 sha256:<64 hex>。
func SHA256Digest(v any) (string, error) {
	raw, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalize(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return nil, err
	}
	return canonicalizeValue(decoded)
}

func canonicalizeValue(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			if k == "digest" || k == "payload_hash" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(keys))
		for _, k := range keys {
			cv, err := canonicalizeValue(t[k])
			if err != nil {
				return nil, err
			}
			out[k] = cv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			cv, err := canonicalizeValue(item)
			if err != nil {
				return nil, err
			}
			out[i] = cv
		}
		return out, nil
	default:
		return t, nil
	}
}
