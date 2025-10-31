package setruntime

import (
	"time"

	"github.com/spf13/viper"
)

type RuntimeOptions struct {
	ClientChanCap          int
	ReceiveChanCap         int
	ChangeChanCap          int
	CommandChanCap         int
	Timeout                time.Duration
	ClientTimeout          time.Duration
	ClientTimeoutCheckFreq time.Duration
	setChangeCheckFreq     time.Duration
}

func DefaultRuntimeOptions() RuntimeOptions {
	return RuntimeOptions{
		ClientChanCap:          100,
		ReceiveChanCap:         100,
		ChangeChanCap:          20,
		CommandChanCap:         20,
		Timeout:                30 * time.Minute,
		ClientTimeout:          10 * time.Minute,
		ClientTimeoutCheckFreq: 30 * time.Second,
		setChangeCheckFreq:     30 * time.Second,
	}
}

// TODO : config variables

// RuntimeOptionsFromConfig creates RuntimeOptions from configuration
// Falls back to default values if config keys are not set
func RuntimeOptionsFromConfig() RuntimeOptions {
	// Set default values for viper
	viper.SetDefault("eventruntime.client_chan_cap", 100)
	viper.SetDefault("eventruntime.receive_chan_cap", 100)
	viper.SetDefault("eventruntime.change_chan_cap", 20)
	viper.SetDefault("eventruntime.command_chan_cap", 20)
	viper.SetDefault("eventruntime.timeout", 30*time.Minute)
	viper.SetDefault("eventruntime.cart_timeout", 15*time.Minute)
	viper.SetDefault("eventruntime.cart_expire_check_freq", 30*time.Second)
	viper.SetDefault("eventruntime.client_timeout", 10*time.Minute)
	viper.SetDefault("eventruntime.client_timeout_check_freq", 30*time.Second)
	viper.SetDefault("eventruntime.event_change_check_freq", 30*time.Second)

	return RuntimeOptions{
		ClientChanCap:          viper.GetInt("eventruntime.client_chan_cap"),
		ReceiveChanCap:         viper.GetInt("eventruntime.receive_chan_cap"),
		ChangeChanCap:          viper.GetInt("eventruntime.change_chan_cap"),
		CommandChanCap:         viper.GetInt("eventruntime.command_chan_cap"),
		Timeout:                viper.GetDuration("eventruntime.timeout"),
		ClientTimeout:          viper.GetDuration("eventruntime.client_timeout"),
		ClientTimeoutCheckFreq: viper.GetDuration("eventruntime.client_timeout_check_freq"),
		setChangeCheckFreq:     viper.GetDuration("eventruntime.event_change_check_freq"),
	}
}
