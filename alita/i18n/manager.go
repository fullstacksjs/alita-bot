package i18n

import (
	"embed"
	"fmt"
	"sync"
)

const (
	// DefaultLanguage is the only user-facing locale the bot ships.
	DefaultLanguage = "en"
	// ConfigLanguage is the pseudo-locale backed by locales/config.yml. It holds
	// command alt_names and db_default_* strings rather than user-facing text.
	ConfigLanguage = "config"
)

var (
	managerInstance *LocaleManager
	managerOnce     sync.Once
)

// GetManager returns the process-wide LocaleManager singleton.
func GetManager() *LocaleManager {
	managerOnce.Do(func() {
		managerInstance = &LocaleManager{
			localeMaps: make(map[string]map[string]any),
		}
	})
	return managerInstance
}

// Initialize loads the embedded locale files. It is called once from main().
func (lm *LocaleManager) Initialize(fs *embed.FS, localePath string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Prevent re-initialization
	if lm.localeFS != nil {
		return fmt.Errorf("locale manager already initialized")
	}

	lm.localeFS = fs
	lm.localePath = localePath

	if err := lm.loadLocaleFiles(); err != nil {
		return NewI18nError("initialize", "", "", "failed to load locale files", err)
	}

	if _, exists := lm.localeMaps[DefaultLanguage]; !exists {
		return NewI18nError("initialize", DefaultLanguage, "", "default language not found", ErrLocaleNotFound)
	}

	return nil
}

// getTranslator returns a translator for an already-loaded locale map.
func (lm *LocaleManager) getTranslator(langCode string) (*Translator, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if lm.localeFS == nil {
		return nil, NewI18nError("get_translator", langCode, "", "manager not initialized", ErrManagerNotInit)
	}

	data, exists := lm.localeMaps[langCode]
	if !exists {
		return nil, NewI18nError("get_translator", langCode, "", "locale not loaded", ErrLocaleNotFound)
	}

	return &Translator{
		langCode: langCode,
		manager:  lm,
		data:     data,
	}, nil
}
