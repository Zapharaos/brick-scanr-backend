package wsruntime

import (
	"net/http"
	"strings"

	"github.com/spf13/viper"
)

// CheckOrigin validates the WebSocket upgrade request's Origin header against
// the cors.allowed_origins list in config. An empty origin list allows all
// origins (useful in dev when cors.allowed_origins is not set).
func CheckOrigin(r *http.Request) bool {
	allowed := viper.GetStringSlice("cors.allowed_origins")
	if len(allowed) == 0 {
		return true
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		// No Origin header means same-origin or a non-browser client; allow.
		return true
	}

	for _, a := range allowed {
		if strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}
