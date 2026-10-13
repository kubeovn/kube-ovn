//go:build !linux

package kohelper

import (
	"context"
	"errors"
	"io"
)

func RunEnvironment(context.Context, io.Writer, io.Writer) error {
	return errors.New("environment checks are only available on Linux nodes")
}
