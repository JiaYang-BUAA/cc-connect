package weixin

import (
	"encoding/json"
	"fmt"
	"strings"
)

const codexQuoteFooter = "↩ 引用此条信息进行回复"

func isMediaItemType(t int) bool {
	switch t {
	case messageItemImage, messageItemVoice, messageItemFile, messageItemVideo:
		return true
	default:
		return false
	}
}

// bodyFromItemList extracts user-visible text from Weixin item_list (text, quotes, voice ASR).
func bodyFromItemList(items []messageItem) string {
	if len(items) == 0 {
		return ""
	}
	for _, item := range items {
		switch item.Type {
		case messageItemText:
			if item.TextItem == nil {
				continue
			}
			text := strings.TrimSpace(item.TextItem.Text)
			ref := item.RefMsg
			if ref == nil && item.TextItem != nil {
				ref = item.TextItem.RefMsg
			}
			if ref == nil {
				return text
			}
			if ref.MessageItem != nil && isMediaItemType(ref.MessageItem.Type) {
				return text
			}
			var parts []string
			if ref.Title != "" {
				parts = append(parts, ref.Title)
			}
			if ref.MessageItem != nil {
				refBody := bodyFromItemList([]messageItem{*ref.MessageItem})
				if refBody != "" {
					parts = append(parts, refBody)
				}
			}
			if len(parts) == 0 {
				return text
			}
			return fmt.Sprintf("[引用: %s]\n%s", strings.Join(parts, " | "), text)
		case messageItemVoice:
			if item.VoiceItem != nil && strings.TrimSpace(item.VoiceItem.Text) != "" {
				return strings.TrimSpace(item.VoiceItem.Text)
			}
		}
	}
	return ""
}

// quotedTextReply extracts the quoted outbound text separately from the user's
// reply. Keeping these fields separate lets a local router identify the target
// Codex thread without sending the quote itself to cc-connect's normal agent.
func quotedTextReply(items []messageItem) (quoteText, replyText string, ok bool) {
	for _, item := range items {
		if item.Type != messageItemText || item.TextItem == nil {
			continue
		}
		replyText = strings.TrimSpace(item.TextItem.Text)
		ref := item.RefMsg
		if ref == nil && item.TextItem != nil {
			ref = item.TextItem.RefMsg
		}
		if ref != nil {
			if ref.MessageItem != nil {
				quoteText = strings.TrimSpace(bodyFromItemList([]messageItem{*ref.MessageItem}))
			}
			if quoteText == "" {
				quoteText = strings.TrimSpace(ref.Title)
			}
		}
		if !isCodexQuoteCandidate(quoteText) {
			if rawQuote := codexNotificationFromRaw(item.Raw); rawQuote != "" {
				quoteText = rawQuote
			}
		}
		if quoteText != "" && replyText != "" {
			return quoteText, replyText, true
		}
	}
	return "", "", false
}

func plainTextReply(items []messageItem) string {
	for _, item := range items {
		if item.Type == messageItemText && item.TextItem != nil {
			return strings.TrimSpace(item.TextItem.Text)
		}
	}
	return ""
}

func quotedMessageReference(items []messageItem) (messageID string, createTimeMs int64, ok bool) {
	for _, item := range items {
		if item.Type != messageItemText || item.TextItem == nil {
			continue
		}
		ref := item.RefMsg
		if ref == nil {
			ref = item.TextItem.RefMsg
		}
		if ref == nil || ref.MessageItem == nil {
			continue
		}
		messageID = strings.TrimSpace(ref.MessageItem.MsgID)
		createTimeMs = ref.MessageItem.CreateTimeMs
		if messageID != "" || createTimeMs > 0 {
			return messageID, createTimeMs, true
		}
	}
	return "", 0, false
}

func codexNotificationFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	var titleOnly string
	var find func(any) string
	find = func(current any) string {
		switch typed := current.(type) {
		case string:
			if isCodexNotificationQuote(typed) {
				return strings.TrimSpace(typed)
			}
			if titleOnly == "" && hasCodexChatTitle(typed) {
				titleOnly = strings.TrimSpace(typed)
			}
		case []any:
			for _, entry := range typed {
				if result := find(entry); result != "" {
					return result
				}
			}
		case map[string]any:
			for _, entry := range typed {
				if result := find(entry); result != "" {
					return result
				}
			}
		}
		return ""
	}
	if result := find(value); result != "" {
		return result
	}
	return titleOnly
}

func isCodexNotificationQuote(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.Contains(trimmed, codexQuoteFooter) {
		return false
	}
	return hasCodexChatTitle(trimmed)
}

func hasCodexChatTitle(text string) bool {
	trimmed := strings.TrimSpace(text)
	// Weixin may prepend the sender name (for example, "Codex: ") to the
	// referenced body, so the notification title need not start at byte zero.
	titleStart := strings.Index(trimmed, "【")
	if titleStart < 0 {
		return false
	}
	titleEnd := strings.Index(trimmed[titleStart:], "】")
	return titleEnd > 1
}

func isCodexQuoteCandidate(text string) bool {
	return isCodexNotificationQuote(text) || hasCodexChatTitle(text)
}
