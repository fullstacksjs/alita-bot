package modules

import (
	"context"
	"fmt"
	"time"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

const overwriteCacheTTL = 5 * time.Minute

// overwriteBase holds common fields for temporary state storage during command flows.
type overwriteBase struct {
	ChatID   int64
	UserID   int64
	ItemName string // filterWord or noteWord
	Text     string
	FileID   string
	Buttons  []db.Button
	DataType int
}

// struct for filters module
type overwriteFilter struct {
	overwriteBase
}

// struct for notes module
type overwriteNote struct {
	overwriteBase
	PvtOnly     bool
	GrpOnly     bool
	AdminOnly   bool
	WebPrev     bool
	IsProtected bool
	NoNotif     bool
}

func overwriteCacheKey(kind, token string) string {
	return fmt.Sprintf("alita:%s_overwrite:%s", kind, token)
}

func setOverwriteCache(key string, data any) error {
	state.Set(context.Background(), key, data, overwriteCacheTTL)
	return nil
}

func getOverwriteCache[T any](key string) (*T, error) {
	data, ok := state.Get[T](context.Background(), key)
	if !ok {
		return nil, fmt.Errorf("overwrite cache missed or expired")
	}
	return &data, nil
}

func consumeOverwriteCache[T any](key string) (*T, error) {
	data, ok := state.GetAndDelete[T](context.Background(), key)
	if !ok {
		return nil, fmt.Errorf("overwrite cache missed or expired")
	}
	return &data, nil
}

func deleteOverwriteCache(key string) {
	state.Delete(context.Background(), key)
}
