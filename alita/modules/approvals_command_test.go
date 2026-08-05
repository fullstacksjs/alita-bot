package modules

import (
	"errors"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/divkix/Alita_Robot/alita/db/approvals"
)

func TestApproveApprovalListAndUnapproveCommands(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Approval Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	approveCtx := newModuleMessageContext(bot, chat, admin, "/approve 42 trusted")
	if err := approvalsModule.approveUser(bot, approveCtx); err != ext.EndGroups {
		t.Fatalf("approveUser error = %v, want EndGroups", err)
	}
	if !approvals.IsUserApproved(chat.Id, 42) {
		t.Fatal("user was not approved")
	}
	approved := approvals.GetApprovedUsers(chat.Id)
	if len(approved) != 1 || approved[0].Reason != "trusted" {
		t.Fatalf("approved users = %+v, want reason trusted", approved)
	}

	statusCtx := newModuleMessageContext(bot, chat, admin, "/approval 42")
	if err := approvalsModule.checkApprovalStatus(bot, statusCtx); err != ext.EndGroups {
		t.Fatalf("checkApprovalStatus error = %v, want EndGroups", err)
	}

	listCtx := newModuleMessageContext(bot, chat, admin, "/approved")
	if err := approvalsModule.listApprovedUsers(bot, listCtx); err != ext.EndGroups {
		t.Fatalf("listApprovedUsers error = %v, want EndGroups", err)
	}
	calls := client.callsFor("sendMessage")
	if len(calls) < 3 {
		t.Fatalf("sendMessage calls = %d, want approve, status, and list", len(calls))
	}
	lastText := calls[len(calls)-1].Params["text"].(string)
	if !strings.Contains(lastText, "trusted") {
		t.Fatalf("approved list text = %q, want reason", lastText)
	}

	unapproveCtx := newModuleMessageContext(bot, chat, admin, "/unapprove 42")
	if err := approvalsModule.unapproveUser(bot, unapproveCtx); err != ext.EndGroups {
		t.Fatalf("unapproveUser error = %v, want EndGroups", err)
	}
	if approvals.IsUserApproved(chat.Id, 42) {
		t.Fatal("user stayed approved after /unapprove")
	}
}

func TestApprovalCommandsHandleMissingAndDuplicateUsers(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Approval Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	if err := approvals.AddApprovedUser(chat.Id, 42, admin.Id, "already"); err != nil {
		t.Fatalf("AddApprovedUser setup error = %v", err)
	}

	missingApproveCtx := newModuleMessageContext(bot, chat, admin, "/approve")
	if err := approvalsModule.approveUser(bot, missingApproveCtx); err != ext.EndGroups {
		t.Fatalf("approveUser missing error = %v, want EndGroups", err)
	}

	duplicateCtx := newModuleMessageContext(bot, chat, admin, "/approve 42 again")
	if err := approvalsModule.approveUser(bot, duplicateCtx); err != ext.EndGroups {
		t.Fatalf("approveUser duplicate error = %v, want EndGroups", err)
	}
	if got := len(approvals.GetApprovedUsers(chat.Id)); got != 1 {
		t.Fatalf("approved users after duplicate = %d, want 1", got)
	}

	notApprovedCtx := newModuleMessageContext(bot, chat, admin, "/unapprove 43")
	if err := approvalsModule.unapproveUser(bot, notApprovedCtx); err != ext.EndGroups {
		t.Fatalf("unapproveUser missing approval error = %v, want EndGroups", err)
	}

	statusMissingCtx := newModuleMessageContext(bot, chat, admin, "/approval 43")
	if err := approvalsModule.checkApprovalStatus(bot, statusMissingCtx); err != ext.EndGroups {
		t.Fatalf("checkApprovalStatus missing approval error = %v, want EndGroups", err)
	}
}

func TestApprovedListHandlesEmptyAndLargeLists(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Approval Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	emptyCtx := newModuleMessageContext(bot, chat, admin, "/approved")
	if err := approvalsModule.listApprovedUsers(bot, emptyCtx); err != ext.EndGroups {
		t.Fatalf("listApprovedUsers empty error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want empty-list reply", len(calls))
	}

	for i := 0; i < approvedUsersInlineLimit+1; i++ {
		userID := int64(10_000 + i)
		if err := approvals.AddApprovedUser(chat.Id, userID, admin.Id, "bulk reason"); err != nil {
			t.Fatalf("AddApprovedUser(%d) error = %v", userID, err)
		}
	}

	largeCtx := newModuleMessageContext(bot, chat, admin, "/approved")
	if err := approvalsModule.listApprovedUsers(bot, largeCtx); err != ext.EndGroups {
		t.Fatalf("listApprovedUsers large error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendDocument"); len(calls) != 1 {
		t.Fatalf("sendDocument calls = %d, want large-list document", len(calls))
	}
}

func TestApprovalCommandsPropagateGotgbotRequestErrors(t *testing.T) {
	requestErr := errors.New("telegram request failed")
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	for _, tt := range []struct {
		name   string
		text   string
		method string
		setup  func(t *testing.T, chat gotgbot.Chat)
		run    func(*gotgbot.Bot, *ext.Context) error
	}{
		{name: "approve missing target reply", text: "/approve", method: "sendMessage", run: approvalsModule.approveUser},
		{name: "approve success reply", text: "/approve 42 trusted", method: "sendMessage", run: approvalsModule.approveUser},
		{
			name:   "approve duplicate reply",
			text:   "/approve 42 again",
			method: "sendMessage",
			setup: func(t *testing.T, chat gotgbot.Chat) {
				t.Helper()
				if err := approvals.AddApprovedUser(chat.Id, 42, admin.Id, "already"); err != nil {
					t.Fatalf("AddApprovedUser setup error = %v", err)
				}
			},
			run: approvalsModule.approveUser,
		},
		{name: "unapprove missing target reply", text: "/unapprove", method: "sendMessage", run: approvalsModule.unapproveUser},
		{name: "unapprove not approved reply", text: "/unapprove 42", method: "sendMessage", run: approvalsModule.unapproveUser},
		{
			name:   "unapprove success reply",
			text:   "/unapprove 42",
			method: "sendMessage",
			setup: func(t *testing.T, chat gotgbot.Chat) {
				t.Helper()
				if err := approvals.AddApprovedUser(chat.Id, 42, admin.Id, "trusted"); err != nil {
					t.Fatalf("AddApprovedUser setup error = %v", err)
				}
			},
			run: approvalsModule.unapproveUser,
		},
		{name: "approval missing target reply", text: "/approval", method: "sendMessage", run: approvalsModule.checkApprovalStatus},
		{name: "approval not approved reply", text: "/approval 42", method: "sendMessage", run: approvalsModule.checkApprovalStatus},
		{
			name:   "approval status reply",
			text:   "/approval 42",
			method: "sendMessage",
			setup: func(t *testing.T, chat gotgbot.Chat) {
				t.Helper()
				if err := approvals.AddApprovedUser(chat.Id, 42, admin.Id, "trusted"); err != nil {
					t.Fatalf("AddApprovedUser setup error = %v", err)
				}
			},
			run: approvalsModule.checkApprovalStatus,
		},
		{name: "approved empty list reply", text: "/approved", method: "sendMessage", run: approvalsModule.listApprovedUsers},
		{
			name:   "approved inline list reply",
			text:   "/approved",
			method: "sendMessage",
			setup: func(t *testing.T, chat gotgbot.Chat) {
				t.Helper()
				if err := approvals.AddApprovedUser(chat.Id, 42, admin.Id, "trusted"); err != nil {
					t.Fatalf("AddApprovedUser setup error = %v", err)
				}
			},
			run: approvalsModule.listApprovedUsers,
		},
		{
			name:   "approved large list document",
			text:   "/approved",
			method: "sendDocument",
			setup: func(t *testing.T, chat gotgbot.Chat) {
				t.Helper()
				for i := 0; i < approvedUsersInlineLimit+1; i++ {
					if err := approvals.AddApprovedUser(chat.Id, int64(10_000+i), admin.Id, "bulk"); err != nil {
						t.Fatalf("AddApprovedUser setup error = %v", err)
					}
				}
			},
			run: approvalsModule.listApprovedUsers,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newModuleBotClient()
			bot := newModuleTestBot(client)
			client.errors[tt.method] = requestErr
			chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Approval Chat"}
			if tt.setup != nil {
				tt.setup(t, chat)
			}
			ctx := newModuleMessageContext(bot, chat, admin, tt.text)

			err := tt.run(bot, ctx)
			if !errors.Is(err, requestErr) {
				t.Fatalf("%s returned error %v, want request error", tt.text, err)
			}
		})
	}
}
