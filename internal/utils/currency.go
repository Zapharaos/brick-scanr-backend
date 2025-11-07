package utils

import (
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

var currencyTag language.Tag

func InitCurrency() {
	currency := viper.GetString("default_currency")
	if currency != "" {
		tag, err := language.Parse(currency)
		if err != nil {
			zap.L().Error("Failed to parse currency", zap.String("currency", currency), zap.Error(err))
		}
		currencyTag = tag
		zap.L().Info("Currency loaded", zap.String("currency", currency))
	} else {
		zap.L().Warn("No currency provided")
	}
}

func GetCurrency() language.Tag {
	if currencyTag == (language.Tag{}) {
		return language.AmericanEnglish
	}
	return currencyTag
}
