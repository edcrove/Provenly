package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunWiresTheCLI(t *testing.T) {
	assert.Equal(t, 1, run([]string{"unknown-command"}), "usage errors exit with code 1")
}
