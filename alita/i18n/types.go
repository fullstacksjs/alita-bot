package i18n

import (
	"embed"
	"sync"
)

// TranslationParams represents parameters for translation interpolation
type TranslationParams map[string]any

// LocaleManager holds the parsed locale maps (English plus the `config`
// pseudo-locale) with thread-safe access.
type LocaleManager struct {
	mu         sync.RWMutex
	localeMaps map[string]map[string]any // Parsed YAML maps per locale file
	localeFS   *embed.FS
	localePath string
}

// Translator provides string lookups for a single loaded locale map.
type Translator struct {
	langCode string
	manager  *LocaleManager
	data     map[string]any // Parsed YAML map for this locale
}
