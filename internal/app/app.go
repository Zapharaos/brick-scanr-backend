package app

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/Zapharaos/go-spit"
	"github.com/Zapharaos/lingo"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Init initializes the application.
func Init(version, buildDate string) {
	initializeConfig()

	loggerProduction := viper.GetBool("logger.production")
	initLogger(loggerProduction)
	initLoggerForSpit() // Initialize logger for go-spit

	zap.L().Info("Starting BrickScanr API", zap.String("version", version), zap.String("build_date", buildDate))

	// Initialize utilities
	utils.RunInitWithTime(utils.InitDate, "Initializing Date")
	utils.RunInitWithTime(utils.InitLocale, "Initializing Locale")
	utils.RunInitWithTime(initTranslations, "Initializing Translations")

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

func initTranslations() {
	// Load translations configuration variables
	translationsPath := viper.GetString("translations.config_path")
	filePrefixes := viper.GetStringSlice("translations.file_prefixes")

	// Initialize the translation service
	localizerService, err := lingo.NewI18n(utils.GetLocale(), translationsPath, filePrefixes...)
	if err != nil {
		zap.L().Fatal("Failed to initialize translation service", zap.Error(err))
		return
	}

	// Replace the global translation service instance
	lingo.SetLocalizerService(localizerService)
}

// initLoggerForSpit initializes the logger for go-spit using zap.
func initLoggerForSpit() {
	spitLogger := NewSpitZapLogger(zap.L())
	spit.SetLogger(spitLogger)
	spit.SetLogLevel(spit.LevelInfo)
}
