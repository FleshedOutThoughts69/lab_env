//go:build integration

package cmd

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("LAB_TEST_MODE") != "live" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}