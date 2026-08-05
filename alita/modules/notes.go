package modules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/db/notes"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/content"
	"github.com/divkix/Alita_Robot/alita/utils/extraction"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
	"github.com/divkix/Alita_Robot/alita/utils/helpers"
	"github.com/divkix/Alita_Robot/alita/utils/media"
)

var notesModule = moduleStruct{
	moduleName: "Notes",
}

func noteOverwriteCacheKey(token string) string {
	return overwriteCacheKey("note", token)
}

func setNoteOverwriteCache(token string, data overwriteNote) error {
	return setOverwriteCache(noteOverwriteCacheKey(token), data)
}

func getNoteOverwriteCache(token string) (*overwriteNote, error) {
	return getOverwriteCache[overwriteNote](noteOverwriteCacheKey(token))
}

func consumeNoteOverwriteCache(token string) (*overwriteNote, error) {
	return consumeOverwriteCache[overwriteNote](noteOverwriteCacheKey(token))
}

func deleteNoteOverwriteCache(token string) {
	deleteOverwriteCache(noteOverwriteCacheKey(token))
}

// addNote handles the /save command to create new notes
// with support for various media types and formatting options.
//
//nolint:dupl // addNote shares validation logic with filters module by design
func (m moduleStruct) addNote(b *gotgbot.Bot, ctx *ext.Context) error {
	// connection status
	connectedChat := chat_status.IsUserConnected(b, ctx, true, true)
	if connectedChat == nil {
		return ext.EndGroups
	}
	ctx.EffectiveChat = connectedChat
	chat := ctx.EffectiveChat
	msg := ctx.EffectiveMessage
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}
	args := ctx.Args()

	// check permission
	if !chat_status.CanUserChangeInfo(b, ctx, chat, user.Id) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_change_info_cmd_error", "chat_status_change_info_button_error")
		return ext.EndGroups
	}

	tr := i18n.English()
	noteString, _ := tr.GetString("notes_save_success")

	if msg.ReplyToMessage != nil && len(args) <= 1 {
		tr := i18n.English()
		text, _ := tr.GetString("notes_keyword_required")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	} else if len(args) <= 2 && msg.ReplyToMessage == nil {
		tr := i18n.English()
		text, _ := tr.GetString("notes_invalid")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	result := content.ExtractNoteAndFilter(msg, false)
	noteWord, fileid, text, dataType, buttons, pvtOnly, grpOnly, adminOnly, webPrev, isProtected, noNotif, errorMsg := result.KeyWord, result.FileID, result.Text, result.DataType, result.Buttons, result.PvtOnly, result.GrpOnly, result.AdminOnly, result.WebPreview, result.IsProtected, result.NoNotif, result.ErrorMsg
	if dataType == -1 && errorMsg != "" {
		_, err := msg.Reply(b, errorMsg, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	// if user specifies both noprivate and private, the note will be sent to default.
	// If privatenotes is enabled, the private else group
	if grpOnly && pvtOnly {
		grpOnly, pvtOnly = false, false
		noteConflictText, _ := tr.GetString("notes_private_conflict_warning")
		noteString += noteConflictText
	}

	noteWord = strings.ToLower(noteWord)

	// check if note already exists or not
	if notes.DoesNoteExists(chat.Id, noteWord) {
		token, tokenErr := newOverwriteToken()
		if tokenErr != nil {
			log.Errorf("[Notes] Failed to generate overwrite token: %v", tokenErr)
			tr := i18n.English()
			errorText, _ := tr.GetString("notes_overwrite_token_failed")
			_, _ = msg.Reply(b, errorText, formatting.Shtml())
			return ext.EndGroups
		}
		if err := setNoteOverwriteCache(token, overwriteNote{
			overwriteBase: overwriteBase{
				ChatID:   chat.Id,
				UserID:   user.Id,
				ItemName: noteWord,
				Text:     text,
				FileID:   fileid,
				Buttons:  buttons,
				DataType: dataType,
			},
			PvtOnly:     pvtOnly,
			GrpOnly:     grpOnly,
			AdminOnly:   adminOnly,
			WebPrev:     webPrev,
			IsProtected: isProtected,
			NoNotif:     noNotif,
		}); err != nil {
			log.Errorf("[Notes] Failed to cache overwrite data: %v", err)
			errorText, _ := tr.GetString("notes_overwrite_token_failed")
			_, _ = msg.Reply(b, errorText, formatting.Shtml())
			return ext.EndGroups
		}
		tr := i18n.English()
		overwriteText, _ := tr.GetString("notes_overwrite_confirm")
		yesText, _ := tr.GetString("button_yes")
		noText, _ := tr.GetString("button_no")
		_, err := msg.Reply(b,
			overwriteText,
			&gotgbot.SendMessageOpts{
				ParseMode: formatting.HTML,
				ReplyMarkup: gotgbot.InlineKeyboardMarkup{
					InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
						{
							{
								Text: yesText,
								CallbackData: encodeCallbackData("notes.overwrite", map[string]string{
									"a": "yes",
									"t": token,
								}),
							},
							{
								Text: noText,
								CallbackData: encodeCallbackData("notes.overwrite", map[string]string{
									"a": "no",
									"t": token,
								}),
							},
						},
					},
				},
			},
		)
		if err != nil {
			deleteNoteOverwriteCache(token)
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	// Fix Issue 1: Remove go keyword and handle error synchronously
	if err := notes.AddNote(chat.Id, noteWord, text, fileid, buttons, dataType, pvtOnly, grpOnly, adminOnly, webPrev, isProtected, noNotif); err != nil {
		log.Errorf("[Notes] Failed to add note %s in chat %d: %v", noteWord, chat.Id, err)
		tr := i18n.English()
		errorText, _ := tr.GetString("notes_save_failed")
		_, err := msg.Reply(b, errorText, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	_, err := msg.Reply(b, fmt.Sprintf(noteString, noteWord, noteWord), formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// rmNote handles the /clear command to remove existing notes
// from the chat, requiring admin permissions.
func (moduleStruct) rmNote(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	// connection status
	connectedChat := chat_status.IsUserConnected(b, ctx, true, true)
	if connectedChat == nil {
		return ext.EndGroups
	}
	ctx.EffectiveChat = connectedChat
	chat := ctx.EffectiveChat
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}
	args := ctx.Args()

	if len(args) == 1 {
		tr := i18n.English()
		text, _ := tr.GetString("notes_remove_keyword_required")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	// Extract note word safely to prevent panic
	parts := strings.SplitN(msg.Text, " ", 2)
	if len(parts) < 2 {
		return ext.EndGroups // should not happen due to len(args) check above
	}
	noteWord := strings.TrimLeft(parts[1], "#")

	// check permission
	if !chat_status.CanUserChangeInfo(b, ctx, chat, user.Id) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_change_info_cmd_error", "chat_status_change_info_button_error")
		return ext.EndGroups
	}

	// check if note exists in admin notes as well
	if !slices.Contains(notes.GetNotesList(chat.Id, true), strings.ToLower(noteWord)) {
		tr := i18n.English()
		text, _ := tr.GetString("notes_not_exists")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}
	noteWord, _ = extraction.ExtractQuotes(noteWord, false, true)

	// Fix Issue 2: Add error handling for RemoveNote
	if err := notes.RemoveNote(chat.Id, strings.ToLower(noteWord)); err != nil {
		log.Errorf("[Notes] Failed to remove note %s in chat %d: %v", noteWord, chat.Id, err)
		tr := i18n.English()
		errorText, _ := tr.GetString("error_generic")
		_, err := msg.Reply(b, errorText, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	tr := i18n.English()
	text, _ := tr.GetString("notes_removed_success")
	_, err := msg.Reply(b, fmt.Sprintf(text, noteWord), formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}
	return ext.EndGroups
}

// notesList handles the /notes command to display all available
// notes in the chat with appropriate access controls.
func (moduleStruct) notesList(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	// if command is disabled, return
	if chat_status.CheckDisabledCmd(b, msg, "notes") {
		return ext.EndGroups
	}
	// connection status
	connectedChat := chat_status.IsUserConnected(b, ctx, false, true)
	if connectedChat == nil {
		return ext.EndGroups
	}
	ctx.EffectiveChat = connectedChat
	chat := ctx.EffectiveChat
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}

	noteKeys := notes.GetNotesList(chat.Id, chat_status.RequireUserAdmin(b, ctx, nil, user.Id))
	tr := i18n.English()
	info, _ := tr.GetString("notes_none_in_chat")

	if len(noteKeys) == 0 {
		_, err := msg.Reply(b, info, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	currentNotesText, _ := tr.GetString("notes_current_in_chat")
	info = currentNotesText
	var sb strings.Builder
	for _, note := range noteKeys {
		fmt.Fprintf(&sb, " - <code>#%s</code>\n", note)
	}
	info += sb.String()
	instructionText, _ := tr.GetString("notes_get_instruction")
	info += instructionText
	_, err := msg.Reply(b, info, formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// rmAllNotes handles the /clearall command to remove all notes
// from the chat, restricted to chat owners only.
//
//nolint:dupl // rmAllNotes shares confirmation pattern with filters module by design
func (moduleStruct) rmAllNotes(b *gotgbot.Bot, ctx *ext.Context) error {
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}
	msg := ctx.EffectiveMessage
	chat := ctx.EffectiveChat

	if !chat_status.RequireGroup(b, ctx, nil) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_group_only_error", "", chat_status.WithReply())
		return ext.EndGroups
	}

	// check notes in adminkeys as well
	noteKeys := notes.GetNotesList(chat.Id, true)
	if len(noteKeys) == 0 {
		tr := i18n.English()
		text, _ := tr.GetString("notes_none_in_chat")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	mem, err := chat.GetMember(b, user.Id, nil)
	if err != nil {
		log.Error(err)
		return err
	}

	if mem.MergeChatMember().Status == "creator" {
		tr := i18n.English()
		clearAllText, _ := tr.GetString("notes_clear_all_confirm")
		yesText, _ := tr.GetString("button_yes")
		noText, _ := tr.GetString("button_no")
		_, err := msg.Reply(b, clearAllText,
			&gotgbot.SendMessageOpts{
				ReplyMarkup: gotgbot.InlineKeyboardMarkup{
					InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
						{
							{
								Text:         yesText,
								CallbackData: encodeCallbackData("rmAllNotes", map[string]string{"a": "yes"}),
							},
							{
								Text:         noText,
								CallbackData: encodeCallbackData("rmAllNotes", map[string]string{"a": "no"}),
							},
						},
					},
				},
			},
		)
		if err != nil {
			log.Error(err)
			return err
		}
	} else {
		tr := i18n.English()
		text, _ := tr.GetString("notes_creator_only")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
	}

	return ext.EndGroups
}

// noteOverWriteHandler processes callback queries for note overwrite
// confirmations when adding notes that already exist.
// Callback format:
// - v1 codec: notes.overwrite|v1|a={yes/no}&t={token}
func (m moduleStruct) noteOverWriteHandler(b *gotgbot.Bot, ctx *ext.Context) error {
	query, ok := callbackQueryFromContext(ctx)
	if !ok {
		return ext.EndGroups
	}
	user := query.From

	// permission checks
	if !chat_status.RequireUserAdmin(b, ctx, nil, user.Id) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_user_admin_cmd_error", "chat_status_user_admin_button_error", chat_status.WithReplyFallback())
		return ext.EndGroups
	}

	var helpText string
	action, token, ok := parseOverwriteCallbackData(query.Data, "notes.overwrite")
	if !ok {
		log.WithField("data", query.Data).Warn("Invalid note overwrite callback data format")
		return ext.EndGroups
	}

	tr := i18n.English()
	switch action {
	case "no":
		if token != "" {
			noteData, err := getNoteOverwriteCache(token)
			if err == nil && (noteData.UserID == 0 || noteData.UserID == user.Id) {
				deleteNoteOverwriteCache(token)
			}
		}
		helpText, _ = tr.GetString("notes_overwrite_cancelled")
	case "yes":
		var chatId int64

		if token == "" {
			helpText, _ = tr.GetString("notes_overwrite_cancelled")
			break
		}
		pending, err := getNoteOverwriteCache(token)
		if err != nil || (pending.UserID != 0 && pending.UserID != user.Id) {
			helpText, _ = tr.GetString("notes_overwrite_cancelled")
			break
		}
		noteData, err := consumeNoteOverwriteCache(token)
		if err != nil {
			helpText, _ = tr.GetString("notes_overwrite_cancelled")
			break
		}
		chatId = noteData.ChatID
		if chatId == 0 {
			if query.Message != nil {
				chatId = query.Message.GetChat().Id
			} else if ctx.EffectiveChat != nil {
				chatId = ctx.EffectiveChat.Id
			}
		}

		callbackChatID := int64(0)
		if query.Message != nil {
			callbackChatID = query.Message.GetChat().Id
		} else if ctx.EffectiveChat != nil {
			callbackChatID = ctx.EffectiveChat.Id
		}
		if noteData.ChatID != 0 && callbackChatID != 0 && noteData.ChatID != callbackChatID {
			helpText, _ = tr.GetString("notes_overwrite_cancelled")
			break
		}

		updated, err := notes.UpdateNote(
			chatId,
			noteData.ItemName,
			noteData.Text,
			noteData.FileID,
			noteData.Buttons,
			noteData.DataType,
			noteData.PvtOnly,
			noteData.GrpOnly,
			noteData.AdminOnly,
			noteData.WebPrev,
			noteData.IsProtected,
			noteData.NoNotif,
		)
		if err != nil {
			log.Errorf("[Notes] Failed to update note during overwrite: %v", err)
			helpText, _ = tr.GetString("notes_save_failed")
		} else if updated {
			helpText, _ = tr.GetString("notes_overwrite_success")
		} else {
			helpText, _ = tr.GetString("notes_overwrite_cancelled")
		}
	default:
		log.WithField("action", action).Warn("Unknown note overwrite action")
		text, _ := tr.GetString("common_callback_invalid_request")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		return ext.EndGroups
	}

	if query.Message == nil {
		if _, err := query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: helpText}); err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	_, _, err := query.Message.EditText(
		b,
		helpText,
		nil,
	)
	if err != nil {
		log.Error(err)
		return err
	}

	_, err = query.Answer(b,
		&gotgbot.AnswerCallbackQueryOpts{
			Text: helpText,
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// notesButtonHandler processes callback queries for the remove all notes
// confirmation dialog, restricted to chat owners.
func (moduleStruct) notesButtonHandler(b *gotgbot.Bot, ctx *ext.Context) error {
	query, ok := callbackQueryFromContext(ctx)
	if !ok {
		return ext.EndGroups
	}
	user := query.From

	// permission checks
	if !chat_status.RequireUserOwner(b, ctx, nil, user.Id) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_owner_cmd_error", "chat_status_owner_button_error", chat_status.WithReply())
		return ext.EndGroups
	}

	response := ""
	if decoded, ok := decodeCallbackData(query.Data, "rmAllNotes"); ok {
		response, _ = decoded.Field("a")
	}
	if response == "" {
		log.Warnf("[Notes] Invalid callback data format: %s", query.Data)
		tr := i18n.English()
		text, _ := tr.GetString("common_callback_invalid_request")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		return ext.EndGroups
	}
	var helpText string

	tr := i18n.English()
	chat := ctx.EffectiveChat
	switch response {
	case "yes":
		// Fix Issue 4: Add error handling for RemoveAllNotes
		if chat == nil {
			helpText, _ = tr.GetString("error_generic")
			break
		}
		if err := notes.RemoveAllNotes(chat.Id); err != nil {
			log.Errorf("[Notes] Failed to remove all notes: %v", err)
			helpText, _ = tr.GetString("error_generic")
		} else {
			helpText, _ = tr.GetString("notes_clear_all_success")
		}
	case "no":
		helpText, _ = tr.GetString("notes_clear_all_cancelled")
	default:
		text, _ := tr.GetString("common_callback_invalid_request")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		return ext.EndGroups
	}

	if query.Message == nil {
		if _, err := query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: helpText}); err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	_, _, err := query.Message.EditText(
		b,
		helpText,
		nil,
	)
	if err != nil {
		log.Error(err)
		return err
	}

	_, err = query.Answer(b,
		&gotgbot.AnswerCallbackQueryOpts{
			Text: helpText,
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}
	return ext.EndGroups
}

// notesWatcher monitors messages starting with '#' and automatically
// sends the corresponding note if it exists in the chat.
func (m moduleStruct) notesWatcher(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	chat := ctx.EffectiveChat
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.ContinueGroups
	}

	var replyMsgId int64
	var err error

	if reply := msg.ReplyToMessage; reply != nil {
		replyMsgId = reply.MessageId
	} else {
		replyMsgId = msg.MessageId
	}

	parseText := strings.ToLower(msg.Text)[1:] // remove '#' from note name
	noteNameArgs := strings.Split(parseText, " ")
	noteName := noteNameArgs[0]
	noformatNote := len(noteNameArgs) == 2 && noteNameArgs[1] == "noformat"

	// if note does not exist, continue groups
	if !slices.Contains(notes.GetNotesList(chat.Id, true), strings.ToLower(noteName)) {
		return ext.ContinueGroups
	}

	noteData := notes.GetNote(chat.Id, strings.ToLower(noteName))

	// check if notedata is correct or not
	if noteData.NoteContent == "" && noteData.FileID == "" {
		tr := i18n.English()
		text, _ := tr.GetString("notes_parsing_error")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	// check for admin only notes
	// admin notes follow the group note policy
	if noteData.AdminOnly {
		if !chat_status.IsUserAdmin(b, chat.Id, user.Id) {
			tr := i18n.English()
			text, _ := tr.GetString("notes_admin_only")
			_, err := msg.Reply(b, text, formatting.Shtml())
			if err != nil {
				log.Error(err)
				return err
			}
			return ext.EndGroups
		}
	}

	if noformatNote {
		err = m.sendNoFormatNote(b, ctx, replyMsgId, noteData)
		if err != nil {
			log.Error(err)
			return err
		}
	} else {
		_, err = media.SendNote(b, ctx, chat, noteData, replyMsgId, ctx.Message.MessageThreadId)
	}

	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// sendNoFormatNote sends a note in raw format without markdown processing,
// showing the original formatting codes, restricted to admins.
func (moduleStruct) sendNoFormatNote(b *gotgbot.Bot, ctx *ext.Context, replyMsgId int64, noteData *db.Notes) error {
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		chat_status.NewPermissionResponder(b).Respond(ctx, "common_cannot_identify_user", "", chat_status.WithReply())
		return ext.EndGroups
	}

	// check if user is admin or not
	if !chat_status.RequireUserAdmin(b, ctx, nil, user.Id) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_user_admin_cmd_error", "chat_status_user_admin_button_error", chat_status.WithReplyFallback())
		return ext.EndGroups
	}

	// Reverse notedata
	noteData.NoteContent = formatting.ReverseHTML2MD(noteData.NoteContent)

	// show the buttons back as text
	noteData.NoteContent += content.RevertButtons(noteData.Buttons)

	// Send note using the new media package
	// raw note does not need webpreview
	_, err := media.Send(b, media.Content{
		Text:    noteData.NoteContent,
		FileID:  noteData.FileID,
		MsgType: noteData.MsgType,
		Name:    noteData.NoteName,
	}, media.Options{
		ChatID:            ctx.Message.Chat.Id,
		ReplyMsgID:        replyMsgId,
		ThreadID:          ctx.Message.MessageThreadId,
		Keyboard:          &gotgbot.InlineKeyboardMarkup{InlineKeyboard: nil},
		NoFormat:          true, // noformat mode
		NoNotif:           noteData.NoNotif,
		WebPreview:        false,
		IsProtected:       noteData.IsProtected,
		AllowWithoutReply: true,
	})
	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

// LoadNotes registers all notes module handlers with the dispatcher,
// including note management commands and the notes watcher.
func LoadNotes(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[notesModule.moduleName] = true

	DefaultHelpRegistry().helpableKb[notesModule.moduleName] = [][]gotgbot.InlineKeyboardButton{
		{
			{
				Text:         trS(i18n.English(), "button_formatting"),
				CallbackData: encodeCallbackData("helpq", map[string]string{"m": "Formatting"}),
			},
		},
	} // Adds Formatting kb button to Notes Menu
	dispatcher.AddHandler(handlers.NewCommand("addnote", notesModule.addNote))
	dispatcher.AddHandler(handlers.NewCommand("clear", notesModule.rmNote))
	dispatcher.AddHandler(handlers.NewCommand("rmnote", notesModule.rmNote))
	dispatcher.AddHandler(handlers.NewCommand("notes", notesModule.notesList))
	helpers.AddCmdToDisableable("notes")
	dispatcher.AddHandler(handlers.NewCommand("clearall", notesModule.rmAllNotes))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("rmAllNotes"), notesModule.notesButtonHandler))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("notes.overwrite"), notesModule.noteOverWriteHandler))
	dispatcher.AddHandler(
		handlers.NewMessage(
			func(msg *gotgbot.Message) bool {
				return strings.HasPrefix(msg.Text, "#")
			},
			notesModule.notesWatcher,
		),
	)
}

func init() {
	RegisterLegacyModule("Notes", 160, LoadNotes)
}
