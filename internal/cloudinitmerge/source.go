package cloudinitmerge

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

var preferredCloudInitKeys = []string{
	"userdata",
	"userData",
}

// MergeSecretData merges the given cloud-init secret data with the monitoring bootstrap data.
func MergeSecretData(data map[string][]byte, osType OSType) (map[string][]byte, string, bool, error) {
	if data == nil {
		return nil, "", false, fmt.Errorf("nil secret data")
	}

	key, ok := findCloudInitSecretKey(data)
	if !ok {
		return nil, "", false, fmt.Errorf("no userdata or userData key found")
	}

	raw := string(data[key])

	decoded, wasBase64 := maybeDecodeBase64(raw)

	merged, changed, err := Merge(decoded, osType)
	if err != nil {
		return nil, "", false, err
	}
	if !changed {
		return data, key, false, nil
	}

	out := copyByteMap(data)
	if wasBase64 {
		out[key] = []byte(base64.StdEncoding.EncodeToString([]byte(merged)))
	} else {
		out[key] = []byte(merged)
	}

	return out, key, true, nil
}

// findCloudInitSecretKey finds the cloud-init data key in the given secret data.
func findCloudInitSecretKey(data map[string][]byte) (string, bool) {
	for _, k := range preferredCloudInitKeys {
		if _, ok := data[k]; ok {
			return k, true
		}
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "userdata" || lk == "userdata.yaml" || lk == "cloud-config" {
			return k, true
		}
	}

	return "", false
}

// maybeDecodeBase64 decodes the given string as base64 if it looks like base64.
func maybeDecodeBase64(in string) (string, bool) {
	trimmed := strings.TrimSpace(in)
	if trimmed == "" {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return in, false
	}

	decodedStr := string(decoded)
	if strings.Contains(decodedStr, "#cloud-config") ||
		strings.Contains(decodedStr, "runcmd:") ||
		strings.Contains(decodedStr, "bootcmd:") ||
		strings.Contains(decodedStr, "write_files:") {
		return decodedStr, true
	}

	return in, false
}

// copyByteMap copies the given byte map.
func copyByteMap(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		buf := make([]byte, len(v))
		copy(buf, v)
		out[k] = buf
	}
	return out
}
