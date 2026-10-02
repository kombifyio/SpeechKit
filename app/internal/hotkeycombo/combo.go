// Package hotkeycombo parses platform-neutral hotkey chord strings.
package hotkeycombo

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	vkBackspace = 0x08
	vkTab       = 0x09
	vkReturn    = 0x0D
	vkShift     = 0x10
	vkControl   = 0x11
	vkMenu      = 0x12
	vkEscape    = 0x1B
	vkSpace     = 0x20
	vkXButton1  = 0x05
	vkXButton2  = 0x06
	vkLWin      = 0x5B
)

// ParseStrict parses a user-supplied combo without applying fallback defaults.
func ParseStrict(value string) ([]uint32, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, fmt.Errorf("empty combo")
	}

	parts := strings.Split(strings.ToLower(trimmed), "+")
	result := make([]uint32, 0, len(parts))
	seen := make(map[uint32]struct{}, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			return nil, fmt.Errorf("empty token")
		}

		vk, ok := parseToken(token)
		if !ok {
			return nil, fmt.Errorf("unsupported token %q", token)
		}
		if _, exists := seen[vk]; exists {
			return nil, fmt.Errorf("duplicate token %q", token)
		}
		seen[vk] = struct{}{}
		result = append(result, vk)
	}
	return result, nil
}

func parseToken(token string) (uint32, bool) {
	if vk, ok := tokenVirtualKeys[token]; ok {
		return vk, true
	}
	if len(token) == 1 {
		switch c := token[0]; {
		case c >= 'a' && c <= 'z':
			return uint32(c - 'a' + 'A'), true
		case c >= '0' && c <= '9':
			return uint32(c), true
		}
	}
	if strings.HasPrefix(token, "f") && len(token) <= 3 {
		n, err := strconv.Atoi(token[1:])
		if err == nil && n >= 1 && n <= len(functionKeyVirtualKeys) {
			return functionKeyVirtualKeys[n-1], true
		}
	}
	return 0, false
}

var functionKeyVirtualKeys = [...]uint32{
	0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77,
	0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, 0x7F,
	0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
}

var tokenVirtualKeys = map[string]uint32{
	"alt":       vkMenu,
	"ctrl":      vkControl,
	"control":   vkControl,
	"shift":     vkShift,
	"win":       vkLWin,
	"windows":   vkLWin,
	"cmd":       vkLWin,
	"meta":      vkLWin,
	"space":     vkSpace,
	"enter":     vkReturn,
	"return":    vkReturn,
	"esc":       vkEscape,
	"escape":    vkEscape,
	"tab":       vkTab,
	"backspace": vkBackspace,
	"mouse-x1":  vkXButton1,
	"mouse-x2":  vkXButton2,
	"mb4":       vkXButton1,
	"mb5":       vkXButton2,
}
