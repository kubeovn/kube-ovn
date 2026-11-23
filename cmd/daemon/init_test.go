package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitForOSSkipsGeneveTxOffWhenDisabled(t *testing.T) {
	require.NoError(t, initForOS(false))
}
