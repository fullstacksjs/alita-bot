package i18n

import (
	"embed"
	"errors"
	"fmt"
	"strings"
	"testing"
)

//go:embed testdata/locales/* testdata/locales/nested/* testdata/badlocales/* testdata/nodefault/*
var testLocaleFS embed.FS

// ---- Loader utilities ----

func TestExtractLangCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{name: "yml extension", fileName: "en.yml", want: "en"},
		{name: "yaml extension", fileName: "en.yaml", want: "en"},
		{name: "locale with region", fileName: "pt-BR.yml", want: "pt-BR"},
		{name: "no extension", fileName: "README", want: "README"},
		// filepath.Ext("en.yml.bak")=".bak" -> trim ".bak" -> "en.yml" -> trim ".yml" -> "en"
		{name: "double yml extension", fileName: "en.yml.bak", want: "en"},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractLangCode(tc.fileName)
			if got != tc.want {
				t.Fatalf("extractLangCode(%q) = %q, want %q", tc.fileName, got, tc.want)
			}
		})
	}
}

func TestIsYAMLFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fileName string
		want     bool
	}{
		{name: "yml lowercase", fileName: "en.yml", want: true},
		{name: "yaml lowercase", fileName: "en.yaml", want: true},
		{name: "json extension", fileName: "en.json", want: false},
		{name: "empty string", fileName: "", want: false},
		{name: "yml uppercase", fileName: "en.YML", want: true},
		{name: "yaml uppercase", fileName: "en.YAML", want: true},
		{name: "no extension", fileName: "en", want: false},
		{name: "txt extension", fileName: "en.txt", want: false},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isYAMLFile(tc.fileName)
			if got != tc.want {
				t.Fatalf("isYAMLFile(%q) = %v, want %v", tc.fileName, got, tc.want)
			}
		})
	}
}

func TestParseYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{
			name:    "valid yaml map",
			content: []byte("key: value\n"),
			wantErr: false,
		},
		{
			name:    "valid nested map",
			content: []byte("parent:\n  child: value\n"),
			wantErr: false,
		},
		{
			name:    "invalid yaml syntax",
			content: []byte("{{{"),
			wantErr: true,
		},
		{
			name:    "list root not a map",
			content: []byte("- item1\n- item2\n"),
			wantErr: true,
		},
		{
			name:    "scalar root not a map",
			content: []byte("hello\n"),
			wantErr: true,
		},
		{
			name:    "empty content nil not a map",
			content: []byte(""),
			wantErr: true,
		},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseYAML(tc.content)
			if tc.wantErr && err == nil {
				t.Fatalf("parseYAML() = nil, want non-nil error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("parseYAML() = %v, want nil", err)
			}
		})
	}
}

// ---- Error types ----

func TestI18nErrorFormatWithErr(t *testing.T) {
	t.Parallel()

	base := fmt.Errorf("base error")
	err := NewI18nError("get", "en", "hello", "not found", base)

	msg := err.Error()
	if !strings.Contains(msg, "i18n get failed") {
		t.Fatalf("Error() = %q, want it to contain %q", msg, "i18n get failed")
	}
	if !strings.Contains(msg, "base error") {
		t.Fatalf("Error() = %q, want it to contain %q", msg, "base error")
	}
}

func TestI18nErrorFormatWithoutErr(t *testing.T) {
	t.Parallel()

	err := NewI18nError("get", "en", "hello", "not found", nil)

	msg := err.Error()
	if strings.Contains(msg, "<nil>") {
		t.Fatalf("Error() = %q, should not contain %q", msg, "<nil>")
	}
	if !strings.Contains(msg, "not found") {
		t.Fatalf("Error() = %q, want it to contain %q", msg, "not found")
	}
}

func TestI18nErrorUnwrap(t *testing.T) {
	t.Parallel()

	base := fmt.Errorf("underlying")
	err := NewI18nError("op", "en", "key", "msg", base)

	if !errors.Is(err, base) {
		t.Fatalf("errors.Is(err, base) = false, want true")
	}
}

func TestI18nErrorUnwrapNil(t *testing.T) {
	t.Parallel()

	err := NewI18nError("op", "en", "key", "msg", nil)
	if err.Unwrap() != nil {
		t.Fatalf("Unwrap() = %v, want nil", err.Unwrap())
	}
}

func TestNewI18nError(t *testing.T) {
	t.Parallel()

	base := fmt.Errorf("root cause")
	err := NewI18nError("myOp", "fr", "my.key", "my message", base)

	if err.Op != "myOp" {
		t.Fatalf("Op = %q, want %q", err.Op, "myOp")
	}
	if err.Lang != "fr" {
		t.Fatalf("Lang = %q, want %q", err.Lang, "fr")
	}
	if err.Key != "my.key" {
		t.Fatalf("Key = %q, want %q", err.Key, "my.key")
	}
	if err.Message != "my message" {
		t.Fatalf("Message = %q, want %q", err.Message, "my message")
	}
	if !errors.Is(err.Err, base) {
		t.Fatalf("Err = %v, want %v", err.Err, base)
	}
}

func TestPredefinedErrorsDistinct(t *testing.T) {
	t.Parallel()

	predefined := []struct {
		name string
		err  error
	}{
		{"ErrLocaleNotFound", ErrLocaleNotFound},
		{"ErrKeyNotFound", ErrKeyNotFound},
		{"ErrInvalidYAML", ErrInvalidYAML},
		{"ErrManagerNotInit", ErrManagerNotInit},
	}

	for i, a := range predefined {
		for j, b := range predefined {
			if i == j {
				continue
			}
			if errors.Is(a.err, b.err) {
				t.Fatalf("errors.Is(%s, %s) = true, want false (they must be distinct)", a.name, b.name)
			}
		}
	}
}

func TestPredefinedErrorsChain(t *testing.T) {
	t.Parallel()

	wrapped := NewI18nError("op", "en", "key", "msg", ErrKeyNotFound)
	if !errors.Is(wrapped, ErrKeyNotFound) {
		t.Fatalf("errors.Is(wrapped, ErrKeyNotFound) = false, want true")
	}
}

// ---- Translator utilities ----

func TestExtractOrderedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params TranslationParams
		want   []any
	}{
		{
			name:   "numbered keys 0 1 2",
			params: TranslationParams{"0": "a", "1": "b", "2": "c"},
			want:   []any{"a", "b", "c"},
		},
		{
			name:   "common keys first second",
			params: TranslationParams{"first": "x", "second": "y"},
			want:   []any{"x", "y"},
		},
		{
			name:   "nil params",
			params: nil,
			want:   nil,
		},
		{
			name:   "empty params",
			params: TranslationParams{},
			want:   nil,
		},
		{
			name:   "gap in numbered keys breaks at 1",
			params: TranslationParams{"0": "a", "2": "c"},
			want:   []any{"a"},
		},
		{
			name:   "numbered keys take priority over common",
			params: TranslationParams{"0": "a", "1": "b", "first": "x"},
			want:   []any{"a", "b"},
		},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractOrderedValues(tc.params)
			if len(got) != len(tc.want) {
				t.Fatalf("extractOrderedValues() len = %d, want %d; got %v", len(got), len(tc.want), got)
			}
			for i, v := range tc.want {
				if got[i] != v {
					t.Fatalf("extractOrderedValues()[%d] = %v, want %v", i, got[i], v)
				}
			}
		})
	}
}

// ---- Translator.GetString ----

// newTestTranslator creates a Translator backed by inline YAML content for tests.
func newTestTranslator(t *testing.T, yamlContent string) *Translator {
	t.Helper()
	data, err := parseYAML([]byte(yamlContent))
	if err != nil {
		t.Fatalf("parseYAML() error = %v", err)
	}
	lm := &LocaleManager{
		localeMaps: map[string]map[string]any{DefaultLanguage: data},
	}
	return &Translator{
		langCode: DefaultLanguage,
		manager:  lm,
		data:     data,
	}
}

func TestTranslatorGet(t *testing.T) {
	t.Parallel()

	const yamlContent = `language_name: English
greeting: "Hello, World!"
templ: "Hello, %s!"
`

	t.Run("existing key returns translated string", func(t *testing.T) {
		t.Parallel()

		tr := newTestTranslator(t, yamlContent)
		result, err := tr.GetString("language_name")
		if err != nil {
			t.Fatalf("GetString(language_name) error = %v", err)
		}
		if result != "English" {
			t.Fatalf("GetString(language_name) = %q, want %q", result, "English")
		}
	})

	t.Run("nonexistent key returns error", func(t *testing.T) {
		t.Parallel()

		tr := newTestTranslator(t, yamlContent)
		_, err := tr.GetString("nonexistent_key_xyz")
		if err == nil {
			t.Fatal("GetString(nonexistent_key) expected error, got nil")
		}
		if !errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("expected ErrKeyNotFound, got: %v", err)
		}
	})

	t.Run("key with params substitutes correctly", func(t *testing.T) {
		t.Parallel()

		tr := newTestTranslator(t, yamlContent)
		result, err := tr.GetString("templ", TranslationParams{"0": "Alice"})
		if err != nil {
			t.Fatalf("GetString(templ, params) error = %v", err)
		}
		if !strings.Contains(result, "Alice") {
			t.Fatalf("GetString(templ) = %q, want it to contain 'Alice'", result)
		}
	})

	t.Run("nil params map does not panic", func(t *testing.T) {
		t.Parallel()

		tr := newTestTranslator(t, yamlContent)
		// Calling with explicit nil params should not panic
		result, err := tr.GetString("greeting", nil)
		if err != nil {
			t.Fatalf("GetString(greeting, nil) error = %v", err)
		}
		if result == "" {
			t.Fatal("expected non-empty result")
		}
	})
}

// ---- Locale resolution ----

func TestEnglishAndConfigNeverReturnNil(t *testing.T) {
	t.Parallel()

	// The package singleton is not initialized in unit tests, so both helpers
	// degrade to a bare translator rather than returning nil.
	if English() == nil {
		t.Fatal("English() returned nil")
	}
	if Config() == nil {
		t.Fatal("Config() returned nil")
	}
}

func TestGetTranslatorRequiresInitializedManager(t *testing.T) {
	t.Parallel()

	data, err := parseYAML([]byte("language_name: English\n"))
	if err != nil {
		t.Fatalf("parseYAML() error = %v", err)
	}

	lm := &LocaleManager{
		localeMaps: map[string]map[string]any{DefaultLanguage: data},
	}

	// localeFS is nil so getTranslator returns ErrManagerNotInit.
	if _, getErr := lm.getTranslator(DefaultLanguage); !errors.Is(getErr, ErrManagerNotInit) {
		t.Fatalf("getTranslator() error = %v, want ErrManagerNotInit", getErr)
	}
}

func newTestLocaleManager() *LocaleManager {
	return &LocaleManager{
		localeMaps: make(map[string]map[string]any),
	}
}

func TestLocaleManagerInitializeLoadsEmbeddedLocales(t *testing.T) {
	t.Parallel()

	lm := newTestLocaleManager()
	if err := lm.Initialize(&testLocaleFS, "testdata/locales"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if _, ok := lm.localeMaps["ignored"]; ok {
		t.Fatal("Initialize() loaded a non-YAML entry")
	}
	if _, ok := lm.localeMaps["skipped"]; ok {
		t.Fatal("Initialize() descended into a nested directory")
	}

	en, err := lm.getTranslator(DefaultLanguage)
	if err != nil {
		t.Fatalf("getTranslator(en) error = %v", err)
	}

	items, err := en.GetStringSlice("items")
	if err != nil {
		t.Fatalf("GetStringSlice(items) error = %v", err)
	}
	if len(items) != 2 || items[0] != "one" || items[1] != "two" {
		t.Fatalf("GetStringSlice(items) = %v, want [one two]", items)
	}

	// Unknown locales are no longer silently mapped onto English.
	if _, err := lm.getTranslator("missing"); !errors.Is(err, ErrLocaleNotFound) {
		t.Fatalf("getTranslator(missing) error = %v, want ErrLocaleNotFound", err)
	}

	if err := lm.Initialize(&testLocaleFS, "testdata/locales"); err == nil {
		t.Fatal("second Initialize() call returned nil, want already initialized error")
	}
}

func TestLocaleManagerInitializeReturnsLoadError(t *testing.T) {
	t.Parallel()

	lm := newTestLocaleManager()
	err := lm.Initialize(&testLocaleFS, "testdata/badlocales")
	if err == nil {
		t.Fatal("Initialize() with invalid locale returned nil error")
	}
	if !strings.Contains(err.Error(), "failed to load locale files") {
		t.Fatalf("Initialize() error = %v, want load failure", err)
	}
}

func TestLocaleManagerInitializeRequiresDefaultLanguage(t *testing.T) {
	t.Parallel()

	lm := newTestLocaleManager()
	err := lm.Initialize(&testLocaleFS, "testdata/nodefault")
	if err == nil {
		t.Fatal("Initialize() without default language returned nil error")
	}
	if !errors.Is(err, ErrLocaleNotFound) {
		t.Fatalf("Initialize() error = %v, want ErrLocaleNotFound", err)
	}
}

func TestLocaleManagerLoadLocaleFilesRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	lm := newTestLocaleManager()
	err := lm.loadLocaleFiles()
	if err == nil {
		t.Fatal("loadLocaleFiles() with nil filesystem returned nil error")
	}
}

// ---- New expanded tests ----

func TestTranslator_GetString_NilManager(t *testing.T) {
	t.Parallel()

	tr := &Translator{langCode: "en", manager: nil}
	_, err := tr.GetString("some_key")
	if err == nil {
		t.Fatal("GetString with nil manager expected error, got nil")
	}
	if !errors.Is(err, ErrManagerNotInit) {
		t.Fatalf("expected ErrManagerNotInit, got: %v", err)
	}
}

func TestTranslator_GetString_ReturnsEnglishValue(t *testing.T) {
	t.Parallel()

	const enYAML = "fallback_key: \"en value\"\n"
	tr := newTestTranslator(t, enYAML)

	result, err := tr.GetString("fallback_key")
	if err != nil {
		t.Fatalf("GetString(fallback_key) error = %v", err)
	}
	if result != "en value" {
		t.Fatalf("GetString(fallback_key) = %q, want %q", result, "en value")
	}
}

func TestTranslator_GetString_NamedParams(t *testing.T) {
	t.Parallel()

	const yamlContent = "greet: \"Hello, {user}!\"\n"
	tr := newTestTranslator(t, yamlContent)

	result, err := tr.GetString("greet", TranslationParams{"user": "Alice"})
	if err != nil {
		t.Fatalf("GetString(greet, {user:Alice}) error = %v", err)
	}
	if !strings.Contains(result, "Alice") {
		t.Fatalf("GetString(greet) = %q, want it to contain %q", result, "Alice")
	}
}

func TestTranslator_GetString_UnusedParams(t *testing.T) {
	t.Parallel()

	const yamlContent = "static: \"no placeholders here\"\n"
	tr := newTestTranslator(t, yamlContent)

	result, err := tr.GetString("static", TranslationParams{"extra": "ignored"})
	if err != nil {
		t.Fatalf("GetString(static, extra params) error = %v", err)
	}
	if result != "no placeholders here" {
		t.Fatalf("GetString(static) = %q, want %q", result, "no placeholders here")
	}
}

func TestTranslator_GetString_EmptyKey(t *testing.T) {
	t.Parallel()

	const yamlContent = "some_key: value\n"
	tr := newTestTranslator(t, yamlContent)

	_, err := tr.GetString("")
	if err == nil {
		t.Fatal("GetString(\"\") expected error, got nil")
	}
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got: %v", err)
	}
}

func TestTranslator_GetStringSlice_NilManager(t *testing.T) {
	t.Parallel()

	tr := &Translator{langCode: "en", manager: nil}
	_, err := tr.GetStringSlice("some_key")
	if err == nil {
		t.Fatal("GetStringSlice with nil manager expected error, got nil")
	}
	if !errors.Is(err, ErrManagerNotInit) {
		t.Fatalf("expected ErrManagerNotInit, got: %v", err)
	}
}

func TestTranslator_GetStringSlice_ExistingAndMissingKeys(t *testing.T) {
	t.Parallel()

	const yamlContent = `
items:
  - one
  - two
`
	tr := newTestTranslator(t, yamlContent)

	items, err := tr.GetStringSlice("items")
	if err != nil {
		t.Fatalf("GetStringSlice(items) error = %v", err)
	}
	if len(items) != 2 || items[0] != "one" || items[1] != "two" {
		t.Fatalf("GetStringSlice(items) = %v, want [one two]", items)
	}

	_, err = tr.GetStringSlice("missing")
	if err == nil {
		t.Fatal("GetStringSlice(missing) error = nil, want ErrKeyNotFound")
	}
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("GetStringSlice(missing) error = %v, want ErrKeyNotFound", err)
	}
}
