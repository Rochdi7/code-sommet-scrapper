package installplaywright

import (
	"context"
	"fmt"

	"github.com/gosom/google-maps-scraper/runner"
)

type installer struct {
}

func New(cfg *runner.Config) (runner.Runner, error) {
	if cfg.RunMode != runner.RunModeInstallPlaywright {
		return nil, fmt.Errorf("%w: %d", runner.ErrInvalidRunMode, cfg.RunMode)
	}

	return &installer{}, nil
}

func (i *installer) Run(context.Context) error {
	// The Playwright driver and browsers are installed at Docker build time and
	// baked into the image (see Dockerfile). Installing at runtime is disabled
	// because the pinned client's download CDN is unreliable in production.
	return nil
}

func (i *installer) Close(context.Context) error {
	return nil
}
