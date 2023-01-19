package testcommands

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.jetpack.io/devbox/examples/testdata/testframework"
)

func TestInfo(t *testing.T) {
	td := testframework.Open()
	output, err := td.Info("notarealpackage", false)
	assert.NoError(t, err)
	assert.Contains(t, output, "Package notarealpackage not found")
	fmt.Println(output + "testsetsts")
}
