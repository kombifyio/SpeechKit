package config

import (
	"strings"
)

func validServerDeviceAgentID(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-', character == ':':
		default:
			return false
		}
	}
	return true
}

func validServerDeviceAgentToken(value string) bool {
	if len(value) < serverDeviceAgentMinimumTokenBytes || len(value) > 512 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '.', character == '_', character == '~':
		case character == '+', character == '/', character == '=':
		default:
			return false
		}
	}
	return true
}

func validServerHomeAssistantToken(value string) bool {
	if len(value) < serverDeviceAgentMinimumTokenBytes || len(value) > 4096 {
		return false
	}
	for _, character := range value {
		if character < '!' || character > '~' {
			return false
		}
	}
	return true
}

func validServerDeviceAgentLanguage(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-':
		default:
			return false
		}
	}
	return true
}

func validServerDeviceAgentLightEntity(raw string) bool {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "light.") || len(value) <= len("light.") || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_':
		default:
			return false
		}
	}
	return true
}
