package modules

import (
	"slices"
	"testing"

	"github.com/divkix/Alita_Robot/alita/i18n"
)

func TestHelpRegistryListsOnlyEnabledModules(t *testing.T) {
	registry := newHelpRegistry()
	registry.AbleMap["Warns"] = true
	registry.AbleMap["Bans"] = true
	registry.AbleMap["Removed"] = false

	if got, want := listModulesFrom(registry), []string{"Bans", "Warns"}; !slices.Equal(got, want) {
		t.Fatalf("listModulesFrom() = %v, want %v", got, want)
	}
}

func TestHelpAliasesResolveRetainedCommands(t *testing.T) {
	registry := newHelpRegistry()
	registry.AbleMap["Bans"] = true

	if got := getModuleNameFromAltName("bans", registry); got != "Bans" {
		t.Fatalf("getModuleNameFromAltName(bans) = %q, want Bans", got)
	}
	if got := getModuleNameFromAltName("rules", registry); got != "" {
		t.Fatalf("removed rules alias resolved to %q", got)
	}
}

func TestHelpKeyboardContainsEnabledModules(t *testing.T) {
	registry := newHelpRegistry()
	registry.AbleMap["Bans"] = true
	registry.AbleMap["Warns"] = true

	keyboard := initHelpButtonsFrom(registry)
	if len(keyboard.InlineKeyboard) != 2 {
		t.Fatalf("keyboard rows = %d, want module row plus back row", len(keyboard.InlineKeyboard))
	}
	if got := []string{keyboard.InlineKeyboard[0][0].Text, keyboard.InlineKeyboard[0][1].Text}; !slices.Equal(got, []string{"Bans", "Warns"}) {
		t.Fatalf("module buttons = %v", got)
	}
}

func TestStartMarkupContainsOnlyHelpNavigation(t *testing.T) {
	markup := getStartMarkup(i18n.English(), "ignored")
	if len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 1 {
		t.Fatalf("start keyboard = %#v, want one help button", markup.InlineKeyboard)
	}
	if _, ok := decodeCallbackData(markup.InlineKeyboard[0][0].CallbackData, "helpq"); !ok {
		t.Fatal("start button does not use help callback namespace")
	}
}
