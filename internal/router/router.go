package router

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/handlers"
	mid "github.com/Zapharaos/brick-scanr-backend/internal/middleware"
	"github.com/Zapharaos/brick-scanr-backend/internal/searchruntime"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type Router struct {
	Router  *chi.Mux
	handler *handlers.Handler
}

func New(setHandler *setruntime.Handler, searchHandler *searchruntime.Handler) *Router {
	r := chi.NewRouter()

	// A good base middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Use(zapChiLogger(zap.L(), "router"))

	// Set a timeout value on the request context (ctx), that will signal
	// through ctx.Done() that the request has timed out and further
	// processing should be stopped.
	r.Use(middleware.Timeout(60 * time.Second))

	// Apply CORS middleware only if enabled in config
	if viper.GetBool("cors.enabled") {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   viper.GetStringSlice("cors.allowed_origins"),
			AllowedMethods:   viper.GetStringSlice("cors.allowed_methods"),
			AllowedHeaders:   viper.GetStringSlice("cors.allowed_headers"),
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: viper.GetBool("cors.allow_credentials"),
			MaxAge:           viper.GetInt("cors.max_age"),
		}))
		zap.L().Info("CORS enabled with origins", zap.Strings("allowed_origins", viper.GetStringSlice("cors.allowed_origins")))
	} else {
		zap.L().Info("CORS disabled")
	}

	router := &Router{
		Router:  r,
		handler: handlers.NewHandler(setHandler, searchHandler),
	}

	// Public health/readiness endpoint for uptime monitoring (e.g. Uptime Kuma)
	// and status displays. Mounted at the root so it's free of the locale
	// middleware and per-IP rate limiting applied under /api/v1.
	r.Get("/health", handlers.Health)
	r.Head("/health", handlers.Health)

	r.Route("/api/v1", func(r chi.Router) {

		// Accept-Language & X-Locale for all routes
		r.Use(mid.LocaleMiddleware)

		// Search endpoints
		r.Get("/search/{query}", router.handler.Search)
		r.Get("/search/ws/{id}", router.handler.SearchWebSocket)

		// Set related endpoints
		r.Route("/set", func(r chi.Router) {

			// Details
			r.With(httprate.LimitByIP(
				viper.GetInt("rate_limit.set_details.requests"),
				time.Duration(viper.GetInt("rate_limit.set_details.window"))*time.Second,
			)).Post("/details/{id}", router.handler.FetchSetDetails)
			r.Get("/details/ws/{id}", router.handler.SetDetailsWebSocket)

			// Export
			r.Post("/export/{id}", handlers.ExportSet)
		})

		// Brick related endpoints
		r.Route("/brick", func(r chi.Router) {

			// Details by design ID
			r.With(httprate.LimitByIP(
				viper.GetInt("rate_limit.brick_details.requests"),
				time.Duration(viper.GetInt("rate_limit.brick_details.window"))*time.Second,
			)).Get("/details/design/{id}", handlers.FetchBrickDetailsByDesign)

			// Details by element ID
			r.With(httprate.LimitByIP(
				viper.GetInt("rate_limit.brick_details.requests"),
				time.Duration(viper.GetInt("rate_limit.brick_details.window"))*time.Second,
			)).Get("/details/element/{id}", handlers.FetchBrickDetailsByElement)
		})
	})

	return router
}

// PrintAllRoutes prints all routes to the console
func (router *Router) PrintAllRoutes() {
	walkFunc := func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		fmt.Printf("%s %s\n", method, route)
		return nil
	}

	if err := chi.Walk(router.Router, walkFunc); err != nil {
		fmt.Printf("Logging err: %s\n", err.Error())
	}
}
