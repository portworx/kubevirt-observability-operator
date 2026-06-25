package sysprepmerge

import (
	"fmt"
	"sort"
	"strings"
)

var preferredKeys = []string{
	"Autounattend.xml",
	"Unattend.xml",
}

// MergeMapData merges the given sysprep data with the monitoring bootstrap data.
func MergeMapData(data map[string]string) (map[string]string, string, bool, error) {
	if data == nil {
		return nil, "", false, fmt.Errorf("nil sysprep data")
	}

	key, ok := findAnswerFileKeyString(data)
	if !ok {
		return nil, "", false, fmt.Errorf("no Autounattend.xml or Unattend.xml key found")
	}

	merged, changed, err := Merge(data[key])
	if err != nil {
		return nil, "", false, err
	}

	if !changed {
		return data, key, false, nil
	}

	out := copyStringMap(data)
	out[key] = merged
	return out, key, true, nil
}

// MergeByteData merges the given sysprep binary data with the monitoring bootstrap data.
func MergeByteData(data map[string][]byte) (map[string][]byte, string, bool, error) {
	if data == nil {
		return nil, "", false, fmt.Errorf("nil sysprep binary data")
	}

	key, ok := findAnswerFileKeyBytes(data)
	if !ok {
		return nil, "", false, fmt.Errorf("no Autounattend.xml or Unattend.xml key found")
	}

	merged, changed, err := Merge(string(data[key]))
	if err != nil {
		return nil, "", false, err
	}

	if !changed {
		return data, key, false, nil
	}

	out := copyByteMap(data)
	out[key] = []byte(merged)
	return out, key, true, nil
}

// findAnswerFileKeyString finds the answer file key in the given sysprep data.
func findAnswerFileKeyString(data map[string]string) (string, bool) {
	for _, k := range preferredKeys {
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
		if strings.HasSuffix(lk, ".xml") && (strings.Contains(lk, "autounattend") || strings.Contains(lk, "unattend")) {
			return k, true
		}
	}

	return "", false
}

// findAnswerFileKeyBytes finds the answer file key in the given sysprep binary data.
func findAnswerFileKeyBytes(data map[string][]byte) (string, bool) {
	for _, k := range preferredKeys {
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
		if strings.HasSuffix(lk, ".xml") && (strings.Contains(lk, "autounattend") || strings.Contains(lk, "unattend")) {
			return k, true
		}
	}

	return "", false
}

// copyStringMap copies the given string map.
func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
