//go:build !linux

package kohelper

import (
	"context"
	"errors"
	"io"
)

func RunCapture(context.Context, []string, io.Writer, io.Writer) error {
	return errors.New("native packet capture is only available on Linux nodes")
}
