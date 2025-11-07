package middleware

import (
	"context"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/app"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// CurrencyMiddleware extracts the currency from the X-Currency or Accept-Currency header
func CurrencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var currency language.Tag
		tag, _, err := language.ParseAcceptLanguage(r.Header.Get("X-Currency"))
		if err != nil || len(tag) == 0 {
			// Default to config if no valid currency is found
			currency = utils.GetCurrency()
		} else {
			// Use the first valid currency from the header
			currency = tag[0]
		}

		zap.L().Debug("Currency middleware",
			zap.String("currency", currency.String()),
			zap.String("path", r.URL.Path),
		)

		ctx := context.WithValue(r.Context(), app.ContextKeyCurrency, currency)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
