package i18n

import (
	log "github.com/sirupsen/logrus"
)

// English returns the translator for the bot's only user-facing locale.
// It never returns nil: when the locale manager is not initialized (unit tests
// that do not load locales) it returns an empty translator whose lookups fail
// cleanly instead of panicking.
func English() *Translator {
	return translatorFor(DefaultLanguage)
}

// Config returns the translator for the `config` pseudo-locale
// (locales/config.yml), which holds alt_names and db_default_* values.
func Config() *Translator {
	return translatorFor(ConfigLanguage)
}

// translatorFor resolves a loaded locale, degrading to an empty translator.
func translatorFor(langCode string) *Translator {
	translator, err := GetManager().getTranslator(langCode)
	if err != nil {
		log.Warnf("Failed to create translator for %s: %v", langCode, err)
		return &Translator{langCode: langCode}
	}
	return translator
}
