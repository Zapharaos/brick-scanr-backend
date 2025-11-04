package app

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/jobs"
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
	utils.RunInitWithTime(initServices, "Initializing Services")
}

// InitServices initializes all handler services
func initServices() {
	// Initialize global job manager singleton
	jobs.ReplaceGlobalManager(jobs.NewManager())

	// Initialize global bricklink client singleton
	bricklink.ReplaceGlobalClient(bricklink.NewClient())
}
