package app

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Init initializes the application.
func Init(version, buildDate string) {
	initializeConfig()

	loggerProduction := viper.GetBool("logger.production")
	initLogger(loggerProduction)

	zap.L().Info("Starting BrickScanr API", zap.String("version", version), zap.String("build_date", buildDate))

	// Initialize utilities
	utils.RunInitWithTime(utils.InitDate, "Initializing Date")
	utils.RunInitWithTime(utils.InitLocale, "Initializing Locale")

	// Initialize Database
	utils.RunInitWithTime(InitRedis, "Initializing Redis")

	// Initialize API clients
	utils.RunInitWithTime(initApiClients, "Initializing API clients")

	// Start databases health monitoring
	database.StartHealthMonitoring("Redis", 30*time.Second, database.DB().Redis(), func() {
		utils.RunInitWithTime(InitRedis, "Health checkup - Initializing Redis")
	})
}

// initApiClients initializes all API clients
func initApiClients() {
	bricklink.ReplaceGlobalClient(bricklink.NewClient())
	pickabrick.ReplaceGlobalClient(pickabrick.NewClient())
	lego.ReplaceGlobalClient(lego.NewClient())
}
