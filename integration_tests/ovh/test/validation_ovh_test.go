package test

import (
	"testing"

	"github.com/CS-SI/SafeScale/integration_tests"
	"github.com/CS-SI/SafeScale/integration_tests/enums/Providers"
)

func Test_Basic(t *testing.T) {
	integration_tests.Basic(t, Providers.OVH)
}

func Test_ReadyToSsh(t *testing.T) {
	integration_tests.ReadyToSsh(t, Providers.OVH)
}

func Test_NasError(t *testing.T) {
	integration_tests.NasError(t, Providers.OVH)
}
