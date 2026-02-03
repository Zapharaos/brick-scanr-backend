package handlers

import (
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/app"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// ParseParamUUIDSoft parses an uuid from the request parameters (using key parameter) without auto http status
func ParseParamUUIDSoft(r *http.Request, key string) (uuid.UUID, bool) {
	value := chi.URLParam(r, key)

	result, err := uuid.Parse(value)
	if err != nil {
		zap.L().Debug("Parse uuid", zap.String("key", key), zap.Error(err))
		return uuid.UUID{}, false
	}

	return result, true
}

// ParseParamUUID parses an uuid from the request parameters (using key parameter)
func ParseParamUUID(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	id, ok := ParseParamUUIDSoft(r, key)
	if !ok {
		render.BadRequest(w, r, fmt.Errorf("invalid %s", key))
	}
	return id, ok
}

// GetLocaleFromContext extracts the locale from the request context
func GetLocaleFromContext(r *http.Request) language.Tag {
	_locale := r.Context().Value(app.ContextKeyLocale)
	if _locale == nil {
		locale := utils.GetLocale()
		zap.L().Warn("No context locale provided, using default", zap.String("locale", locale.String()))
		return locale
	}
	result, ok := _locale.(language.Tag)
	if !ok {
		locale := utils.GetLocale()
		zap.L().Warn("Invalid locale type in context, using default", zap.String("locale", locale.String()))
		return locale
	}
	return result
}

// GetCurrencyFromContext extracts the currency from the request context
func GetCurrencyFromContext(r *http.Request) language.Tag {
	_currency := r.Context().Value(app.ContextKeyCurrency)
	if _currency == nil {
		currency := utils.GetCurrency()
		zap.L().Warn("No context currency provided, using default", zap.String("currency", currency.String()))
		return currency
	}
	result, ok := _currency.(language.Tag)
	if !ok {
		currency := utils.GetCurrency()
		zap.L().Warn("Invalid currency type in context, using default", zap.String("currency", currency.String()))
		return currency
	}
	return result
}
