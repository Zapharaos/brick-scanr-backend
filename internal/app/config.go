package app

import (
	"log"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	configPath = "config"
	configName = "config"
	envPrefix  = "BRICK_SCANR"
)

// initializeConfig initialize viper configuration
func initializeConfig() {
	viper.SetConfigName(configName)
	viper.AddConfigPath(configPath)
	viper.SetConfigType("yaml")

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Cannot read viper configuration: %s", err)
	}

	// Initialize environment variables configuration
	viper.SetEnvPrefix(envPrefix)

	// Replace dots in config keys with underscores for environment variables
	// This allows overriding nested config values with environment variables
	// E.g., checkout.discounts.groups.max_depth becomes BRICK_SCANR_CHECKOUT_DISCOUNTS_GROUPS_MAX_DEPTH
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Enable automatic environment variable binding
	viper.AutomaticEnv()

	// Bind all keys in config to environment variables
	bindEnvs(viper.AllSettings())

	viperDebugFlag := viper.AllSettings()
	delete(viperDebugFlag, "test") // Remove golang standard test flags for cleaner debug
}

// bindEnvs recursively binds each configuration key to its corresponding environment variable
func bindEnvs(settings map[string]interface{}, parentKey ...string) {
	for key, val := range settings {
		// Create the key path
		combinedKey := key
		if len(parentKey) > 0 {
			combinedKey = strings.Join(append(parentKey, key), ".")
		}

		// Bind the combined key to an environment variable
		if err := viper.BindEnv(combinedKey); err != nil {
			zap.L().Error("Failed to bind env var", zap.String("key", combinedKey), zap.Error(err))
		}

		// If this key contains a nested map, recursively bind its keys too
		if nested, ok := val.(map[string]interface{}); ok {
			bindEnvs(nested, append(parentKey, key)...)
		}
	}
}
