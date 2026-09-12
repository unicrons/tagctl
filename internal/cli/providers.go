package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/viper"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/provider/aws"
	// TODO: Enable when ready
	// "github.com/unicrons/tagctl/internal/provider/k8s"
)

// providerAWS is the provider name AWS resources and config entries carry.
const providerAWS = "aws"

// loadConfig loads the config file viper located, or an empty config when none was found.
func loadConfig() (*config.Config, error) {
	path := viper.ConfigFileUsed()
	if path == "" {
		log.Debug("Config: No config file found, using defaults")
		return &config.Config{}, nil
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	log.Debug("Config: Loaded from file: %s", path)
	return cfg, nil
}

// initProviders initializes cloud providers from configuration.
// If regionOverride is provided, it overrides the regions in the config.
// Currently only AWS is supported. GCP, Azure, and Kubernetes coming soon.
func initProviders(ctx context.Context, cfg *config.Config, regionOverride []string) ([]provider.Provider, error) {
	providers := make([]provider.Provider, 0, len(cfg.Clouds.AWS))

	log.Debug("Providers: Found %d AWS account(s)", len(cfg.Clouds.AWS))

	if len(regionOverride) > 0 {
		log.Info("Providers: Using region override: %v", regionOverride)
	}

	// Initialize AWS providers
	for i, account := range cfg.Clouds.AWS {
		// Apply region override if specified
		if len(regionOverride) > 0 {
			account.Regions = regionOverride
		}

		log.Info("Providers: Initializing AWS provider %d/%d (profile=%s, regions=%v)",
			i+1, len(cfg.Clouds.AWS), account.Profile, account.Regions)
		p, err := aws.New(ctx, account)
		if err != nil {
			log.Error("Providers: Failed to initialize AWS provider for profile '%s': %v",
				account.Profile, err)
			return nil, fmt.Errorf("failed to initialize AWS provider for profile '%s': %w",
				account.Profile, err)
		}
		providers = append(providers, p)
	}

	// TODO: Enable Kubernetes providers when ready
	// for i, cluster := range cfg.Clouds.Kubernetes {
	// 	log.Info("Providers: Initializing Kubernetes provider %d/%d (cluster=%s)",
	// 		i+1, len(cfg.Clouds.Kubernetes), cluster.Name)
	// 	p, err := k8s.New(ctx, cluster)
	// 	if err != nil {
	// 		log.Error("Providers: Failed to initialize Kubernetes provider for cluster '%s': %v",
	// 			cluster.Name, err)
	// 		return nil, fmt.Errorf("failed to initialize Kubernetes provider for cluster '%s': %w",
	// 			cluster.Name, err)
	// 	}
	// 	providers = append(providers, p)
	// }

	log.Debug("Providers: Successfully initialized %d provider(s)", len(providers))
	return providers, nil
}

// hasConfiguredProviders checks if any providers are configured.
// Currently only AWS is supported.
func hasConfiguredProviders(cfg *config.Config) bool {
	has := len(cfg.Clouds.AWS) > 0
	log.Debug("Config: hasConfiguredProviders=%v (AWS=%d)", has, len(cfg.Clouds.AWS))
	return has
}

// signalContext is cancelled on Ctrl-C or SIGTERM so in-flight cloud calls
// stop instead of being killed half-way.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
