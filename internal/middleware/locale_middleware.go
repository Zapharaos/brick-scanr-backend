package middleware

import (
	"context"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/app"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"golang.org/x/text/language"
)

// LocaleMiddleware extracts the locale from the Accept-Language header
func LocaleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var locale language.Tag
		tag, _, err := language.ParseAcceptLanguage(r.Header.Get("Accept-Language"))
		if err != nil || len(tag) == 0 {
			// Default to config if no valid locale is found
			locale = utils.GetLocale()
		} else {
			// Use the first valid locale from the Accept-Language header
			locale = tag[0]
		}
		ctx := context.WithValue(r.Context(), app.ContextKeyLocale, locale)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
