//go:build !linux

package cec

import (
	"context"
	"log/slog"
)

// Follow needs the Linux CEC interface; elsewhere it does nothing.
func (c *Controller) Follow(ctx context.Context, _ *slog.Logger) { <-ctx.Done() }
