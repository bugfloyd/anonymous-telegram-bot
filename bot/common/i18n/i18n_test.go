package i18n

import (
	"testing"
)

// TestCompleteTranslations ensures that each language has all required text IDs translated.
func TestCompleteTranslations(t *testing.T) {
	requiredTextIDs := []TextID{
		StartMessageText,
		InitialSendMessagePromptText,
		UnblockButtonText,
		SendMessageButtonText,
		ReplyButtonText,
		BlockButtonText,
		UnblockAllUsersResultText,
		UserBlockedText,
		UserUnblockedText,
	}
	languages := []Language{EnUS, FaIR}

	for _, lang := range languages {
		LoadLocale(lang) // Ensure the locale is loaded for testing

		texts, ok := locales[lang]
		if !ok {
			t.Errorf("locale for language '%s' was not loaded", lang)
			continue
		}

		for _, id := range requiredTextIDs {
			if _, exists := texts[id]; !exists {
				t.Errorf("missing text '%s' for language '%s'", id, lang)
			}
		}
	}
}
