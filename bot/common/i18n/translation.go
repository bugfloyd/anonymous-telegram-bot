package i18n

// TextID represents the keys for internationalized texts
type TextID string

const (
	StartMessageText                TextID = "StartMessageText"
	InitialSendMessagePromptText    TextID = "InitialSendMessagePromptText"
	UnblockButtonText               TextID = "UnblockButtonText"
	SendMessageButtonText           TextID = "SendMessageButtonText"
	ReplyButtonText                 TextID = "ReplyButtonText"
	BlockButtonText                 TextID = "BlockButtonText"
	UnblockAllUsersResultText       TextID = "UnblockAllUsersResultText"
	UserBlockedText                 TextID = "UserBlockedText"
	UserUnblockedText               TextID = "UserUnblockedText"
	CancelButtonText                TextID = "CancelButtonText"
	YourLanguageText                TextID = "YourLanguageText"
	NoPreferredLanguageSetText      TextID = "NoPreferredLanguageSetText"
	NeverMindButtonText             TextID = "NeverMindButtonText"
	LanguageUpdatedSuccessfullyText TextID = "LanguageUpdatedSuccessfullyText"
	YouHaveBlockedThisUserText      TextID = "YouHaveBlockedThisUserText"
	ThisUserHasBlockedYouText       TextID = "ThisUserHasBlockedYouText"
	YouHaveANewMessageText          TextID = "YouHaveANewMessageText"
	NewReplyToYourMessageText       TextID = "NewReplyToYourMessageText"
	OpenMessageButtonText           TextID = "OpenMessageButtonText"
	MessageOpenedText               TextID = "MessageOpenedText"
	ReplyingToMessageText           TextID = "ReplyingToMessageText"
	ReplyToThisMessageText          TextID = "ReplyToThisMessageText"
	UserNotFoundText                TextID = "UserNotFoundText"
	MessageToYourselfTextText       TextID = "MessageToYourselfTextText"
	LinkText                        TextID = "LinkText"
	OrText                          TextID = "OrText"
	InvalidCommandText              TextID = "InvalidCommandText"
	ErrorText                       TextID = "ErrorText"
	YourCurrentUsernameText         TextID = "YourCurrentUsernameText"
	ChangeUsernameButtonText        TextID = "ChangeUsernameButtonText"
	RemoveUsernameButtonText        TextID = "RemoveUsernameButtonText"
	YouDontHaveAUsernameText        TextID = "YouDontHaveAUsernameText"
	SetUsernameButtonText           TextID = "SetUsernameButtonText"
	UsernameExplanationText         TextID = "UsernameExplanationText"
	EnterANewUsernameText           TextID = "EnterANewUsernameText"
	SettingUsernameText             TextID = "SettingUsernameText"
	UsernameHasBeenRemovedText      TextID = "UsernameHasBeenRemovedText"
	InvalidUsernameText             TextID = "InvalidUsernameText"
	UsernameHasBeenSetText          TextID = "UsernameHasBeenSetText"
	UsernameExistsText              TextID = "UsernameExistsText"
	SameUsernameText                TextID = "SameUsernameText"
)

type Language string

const (
	FaIR Language = "fa_IR"
	EnUS Language = "en_US"
)

// LocaleTexts maps Text IDs to their translated strings
type LocaleTexts map[TextID]string

// Locales holds the loaded languages and their texts
var (
	locales       = make(map[Language]LocaleTexts)
	currentLocale Language
)

// SetLocale sets the current language for the application.
func SetLocale(userLanguage Language, clientLanguage string) {
	var language Language
	if userLanguage != "" {
		language = userLanguage
	} else {
		if clientLanguage == "fa" {
			language = FaIR
		} else if clientLanguage == "en" {
			language = EnUS
		} else {
			language = EnUS
		}
	}
	if _, ok := locales[language]; !ok {
		locales[language] = loadLanguage(language)
	}
	currentLocale = language
}

// loadLanguage is a helper function that loads translations for a given language.
func loadLanguage(lang Language) LocaleTexts {
	switch lang {
	case EnUS:
		return enLocale
	case FaIR:
		return faLocale
	}
	return nil
}

// T retrieves a text based on the given language and text ID.
func T(textID TextID) string {
	if text, ok := locales[currentLocale][textID]; ok {
		return text
	}
	return ""
}
