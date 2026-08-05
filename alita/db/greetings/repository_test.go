package greetings

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

func greetingTestChat(t *testing.T) int64 {
	t.Helper()
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
	chatID := time.Now().UnixNano()
	if err := chats.EnsureChatInDb(chatID, "test_greetings"); err != nil {
		t.Fatalf("EnsureChatInDb() error = %v", err)
	}
	t.Cleanup(func() {
		db.DB.Where("chat_id = ?", chatID).Delete(&models.GreetingSettings{})
		db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{})
	})
	return chatID
}

func TestGetGreetingSettingsDefaults(t *testing.T) {
	chatID := greetingTestChat(t)
	settings := GetGreetingSettings(chatID)
	if settings == nil || settings.WelcomeSettings == nil {
		t.Fatal("GetGreetingSettings() returned incomplete welcome settings")
	}
	if !settings.WelcomeSettings.ShouldWelcome {
		t.Fatal("default ShouldWelcome = false, want true")
	}
	if settings.WelcomeSettings.WelcomeText != db.DefaultWelcome {
		t.Fatalf("default WelcomeText = %q, want %q", settings.WelcomeSettings.WelcomeText, db.DefaultWelcome)
	}
}

func TestWelcomeSettingsPersistence(t *testing.T) {
	chatID := greetingTestChat(t)
	buttons := []models.Button{{Name: "docs", Url: "https://example.com"}}

	if err := SetWelcomeText(chatID, "Hello {first}!", "file123", buttons, db.PHOTO); err != nil {
		t.Fatalf("SetWelcomeText() error = %v", err)
	}
	if err := SetWelcomeToggle(chatID, false); err != nil {
		t.Fatalf("SetWelcomeToggle() error = %v", err)
	}
	if err := SetCleanWelcomeSetting(chatID, true); err != nil {
		t.Fatalf("SetCleanWelcomeSetting() error = %v", err)
	}
	if err := SetCleanWelcomeMsgId(chatID, 99999); err != nil {
		t.Fatalf("SetCleanWelcomeMsgId() error = %v", err)
	}
	if err := SetShouldCleanService(chatID, true); err != nil {
		t.Fatalf("SetShouldCleanService() error = %v", err)
	}

	settings := GetGreetingSettings(chatID)
	welcome := settings.WelcomeSettings
	if welcome.ShouldWelcome || !welcome.CleanWelcome || !settings.ShouldCleanService {
		t.Fatalf("persisted toggles = welcome:%t clean:%t service:%t", welcome.ShouldWelcome, welcome.CleanWelcome, settings.ShouldCleanService)
	}
	if welcome.WelcomeText != "Hello {first}!" || welcome.FileID != "file123" || welcome.WelcomeType != db.PHOTO {
		t.Fatalf("persisted welcome = %#v", welcome)
	}
	if welcome.LastMsgId != 99999 || len(welcome.Button) != 1 || welcome.Button[0].Name != "docs" {
		t.Fatalf("persisted cleanup/buttons = %#v", welcome)
	}

	if err := SetWelcomeToggle(chatID, true); err != nil {
		t.Fatalf("SetWelcomeToggle(true) error = %v", err)
	}
	if err := SetCleanWelcomeMsgId(chatID, 0); err != nil {
		t.Fatalf("SetCleanWelcomeMsgId(0) error = %v", err)
	}
	settings = GetGreetingSettings(chatID)
	if !settings.WelcomeSettings.ShouldWelcome || settings.WelcomeSettings.LastMsgId != 0 {
		t.Fatalf("zero-value updates were not persisted: %#v", settings.WelcomeSettings)
	}
}

func TestGetWelcomeButtonsEmpty(t *testing.T) {
	buttons := GetWelcomeButtons(greetingTestChat(t))
	if buttons == nil || len(buttons) != 0 {
		t.Fatalf("GetWelcomeButtons() = %#v, want non-nil empty slice", buttons)
	}
}

func TestLoadGreetingsStats(t *testing.T) {
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
	enabledWelcome, cleanService, cleanWelcome := LoadGreetingsStats()
	if enabledWelcome < 0 || cleanService < 0 || cleanWelcome < 0 {
		t.Fatalf("negative greeting stats: %d %d %d", enabledWelcome, cleanService, cleanWelcome)
	}
}

func TestGreetingSettingsConcurrentWrites(t *testing.T) {
	chatID := greetingTestChat(t)
	const workers = 10
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if err := SetWelcomeToggle(chatID, true); err != nil {
					errs <- fmt.Errorf("SetWelcomeToggle: %w", err)
				}
				return
			}
			if err := SetCleanWelcomeSetting(chatID, true); err != nil {
				errs <- fmt.Errorf("SetCleanWelcomeSetting: %w", err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	settings := GetGreetingSettings(chatID)
	if !settings.WelcomeSettings.ShouldWelcome || !settings.WelcomeSettings.CleanWelcome {
		t.Fatalf("concurrent settings not persisted: %#v", settings.WelcomeSettings)
	}
}

func TestGetGreetingSettingsNonExistentChat(t *testing.T) {
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
	settings := GetGreetingSettings(time.Now().UnixNano())
	if settings == nil || settings.WelcomeSettings == nil || settings.WelcomeSettings.WelcomeText != db.DefaultWelcome {
		t.Fatalf("non-existent chat defaults = %#v", settings)
	}
}
