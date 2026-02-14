package utils

import (
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

var languageTag language.Tag

func InitLocale() {
	locale := viper.GetString("translations.default_locale")
	if locale != "" {
		tag, err := language.Parse(locale)
		if err != nil {
			zap.L().Error("Failed to parse locale", zap.String("locale", locale), zap.Error(err))
		}
		languageTag = tag
		zap.L().Info("Locale loaded", zap.String("locale", locale))
	} else {
		zap.L().Warn("No locale provided")
	}
}

func GetLocale() language.Tag {
	if languageTag == (language.Tag{}) {
		return language.English
	}
	return languageTag
}
