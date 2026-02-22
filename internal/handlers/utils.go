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

// GetLanguageFromContext extracts the language from the request context
func GetLanguageFromContext(r *http.Request) language.Tag {
	_language := r.Context().Value(app.ContextKeyLanguage)
	if _language == nil {
		tag := utils.GetLocale()
		zap.L().Warn("No context language provided, using default", zap.String("language", tag.String()))
		return tag
	}
	result, ok := _language.(language.Tag)
	if !ok {
		tag := utils.GetLocale()
		zap.L().Warn("Invalid language type in context, using default", zap.String("language", tag.String()))
		return tag
	}
	return result
}
