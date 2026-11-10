package device

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// flexBool reads JSON booleans written as true/false, 1/0 or "1"/"0";
// lsblk changed its encoding between util-linux releases.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch strings.ToLower(strings.Trim(string(data), `"`)) {
	case "1", "true":
		*b = true
	case "0", "false", "", "null":
		*b = false
	default:
		return fmt.Errorf("invalid boolean %s", data)
	}
	return nil
}

// flexInt reads integers written either as JSON numbers or as strings.
type flexInt int64

func (n *flexInt) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid integer %s", data)
	}
	*n = flexInt(v)
	return nil
}

// stringList reads a JSON array of strings, a single string or null;
// PowerShell's ConvertTo-Json collapses one-element arrays in some versions.
type stringList []string

func (l *stringList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*l = nil
		return nil
	}
	var items []string
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		items = []string{s}
	} else if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	var out []string
	for _, s := range items {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	*l = out
	return nil
}
