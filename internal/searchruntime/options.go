package searchruntime

import (
	"time"

	"github.com/spf13/viper"
)

type RuntimeOptions struct {
	ClientChanCap          int
	ChangeChanCap          int
	Timeout                time.Duration
	ClientTimeout          time.Duration
	ClientTimeoutCheckFreq time.Duration
}

// RuntimeOptionsFromConfig creates RuntimeOptions from configuration
func RuntimeOptionsFromConfig() RuntimeOptions {
	viper.SetDefault("search.runtime.client_chan_cap", 100)
	viper.SetDefault("search.runtime.change_chan_cap", 50)
	viper.SetDefault("search.runtime.timeout", 5*time.Minute)
	viper.SetDefault("search.runtime.client_timeout", 5*time.Minute)
	viper.SetDefault("search.runtime.client_timeout_check_freq", 30*time.Second)

	return RuntimeOptions{
		ClientChanCap:          viper.GetInt("search.runtime.client_chan_cap"),
		ChangeChanCap:          viper.GetInt("search.runtime.change_chan_cap"),
		Timeout:                viper.GetDuration("search.runtime.timeout"),
		ClientTimeout:          viper.GetDuration("search.runtime.client_timeout"),
		ClientTimeoutCheckFreq: viper.GetDuration("search.runtime.client_timeout_check_freq"),
	}
}
