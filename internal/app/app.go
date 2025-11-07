package app

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
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

	utils.RunInitWithTime(utils.InitDate, "Initializing Date")
	utils.RunInitWithTime(utils.InitLocale, "Initializing Locale")
	utils.RunInitWithTime(utils.InitCurrency, "Initializing Currency")

	utils.RunInitWithTime(InitRedis, "Initializing Redis")
	utils.RunInitWithTime(initServices, "Initializing Services")

	// Start databases health monitoring
	database.StartHealthMonitoring("Redis", 30*time.Second, database.DB().Redis(), func() {
		utils.RunInitWithTime(InitRedis, "Health checkup - Initializing Redis")
	})
}

// InitServices initializes all handler services
func initServices() {
	bricklink.ReplaceGlobalClient(bricklink.NewClient())
	pickabrick.ReplaceGlobalClient(pickabrick.NewClient())
}
