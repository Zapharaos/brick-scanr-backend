package middleware

import (
	"context"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/app"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"golang.org/x/text/language"
)

type Header string

const (
	HeaderAcceptLanguage Header = "Accept-Language"
)

// LocaleMiddleware extracts the locale from the Accept-Language header
func LocaleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tag language.Tag

		// Retrieve accept language header
		tag, ok, err := parseLanguageTagFromHeader(r, HeaderAcceptLanguage)
		if !ok || err != nil {
			// Default to config if no valid locale is found
			tag = utils.GetLocale()
		}
		ctx := context.WithValue(r.Context(), app.ContextKeyLanguage, tag)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// parseLanguageTagFromHeader attempts to parse a language tag from the specified header in the request
func parseLanguageTagFromHeader(r *http.Request, header Header) (language.Tag, bool, error) {
	tag, _, err := language.ParseAcceptLanguage(r.Header.Get(string(header)))
	if err != nil || len(tag) == 0 {
		return language.Tag{}, false, err
	}
	// Use the first valid locale from the Accept-Language header
	return tag[0], true, nil
}
