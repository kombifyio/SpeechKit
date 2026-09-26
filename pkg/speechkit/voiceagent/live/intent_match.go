package live

import (
	"sort"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func isCascadedProvider(provider string) bool {
	provider = NormalizeProviderID(provider)
	return provider == "cascaded" || provider == "local-cascaded"
}

func descriptorSupportsLocale(descriptor ProviderDescriptor, locale string) bool {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		return true
	}
	lang := strings.Split(locale, "-")[0]
	for _, candidate := range descriptor.SupportedLocales {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "*" || candidate == locale || candidate == lang {
			return true
		}
	}
	return len(descriptor.SupportedLocales) == 0
}

func matchedCapabilities(descriptor ProviderDescriptor, capabilities []LiveCapabilityFlag) []LiveCapabilityFlag {
	var out []LiveCapabilityFlag
	for _, capability := range capabilities {
		if descriptor.HasCapability(capability) {
			out = appendCapabilitySet(out, capability)
		}
	}
	sortCapabilityFlags(out)
	return out
}

func missingCapabilities(descriptor ProviderDescriptor, capabilities []LiveCapabilityFlag) []LiveCapabilityFlag {
	var out []LiveCapabilityFlag
	for _, capability := range capabilities {
		if !descriptor.HasCapability(capability) {
			out = appendCapabilitySet(out, capability)
		}
	}
	sortCapabilityFlags(out)
	return out
}

func matchedNativeOptions(descriptor ProviderDescriptor, options []provideropts.OptionID) []provideropts.OptionID {
	var out []provideropts.OptionID
	for _, option := range options {
		if descriptorHasNativeOption(descriptor, option) {
			out = appendOptionSet(out, option)
		}
	}
	sortOptionIDs(out)
	return out
}

func missingNativeOptions(descriptor ProviderDescriptor, options []provideropts.OptionID) []provideropts.OptionID {
	var out []provideropts.OptionID
	for _, option := range options {
		if !descriptorHasNativeOption(descriptor, option) {
			out = appendOptionSet(out, option)
		}
	}
	sortOptionIDs(out)
	return out
}

func descriptorHasNativeOption(descriptor ProviderDescriptor, option provideropts.OptionID) bool {
	for _, candidate := range descriptor.NativeOptions {
		if candidate == option {
			return true
		}
	}
	return false
}

func normalizedProviderList(providers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, provider := range providers {
		normalized := NormalizeProviderID(provider)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func appendCapabilitySet(base []LiveCapabilityFlag, values ...LiveCapabilityFlag) []LiveCapabilityFlag {
	seen := map[LiveCapabilityFlag]bool{}
	for _, value := range base {
		if strings.TrimSpace(string(value)) != "" {
			seen[value] = true
		}
	}
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" || seen[value] {
			continue
		}
		seen[value] = true
		base = append(base, value)
	}
	return base
}

func appendOptionSet(base []provideropts.OptionID, values ...provideropts.OptionID) []provideropts.OptionID {
	seen := map[provideropts.OptionID]bool{}
	for _, value := range base {
		if strings.TrimSpace(string(value)) != "" {
			seen[value] = true
		}
	}
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" || seen[value] {
			continue
		}
		seen[value] = true
		base = append(base, value)
	}
	return base
}

func sortCapabilityFlags(values []LiveCapabilityFlag) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}

func sortOptionIDs(values []provideropts.OptionID) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}

func joinCapabilityFlags(values []LiveCapabilityFlag) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, ",")
}

func joinOptionIDs(values []provideropts.OptionID) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, ",")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
