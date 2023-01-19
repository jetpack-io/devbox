package testrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.jetpack.io/devbox/examples/testdata/testframework"
)

func TestRun(t *testing.T) {
	td := testframework.Open()
	err := td.SetDevboxJson("devbox.json")
	assert.NoError(t, err)
	output, err := td.Run("test1")
	assert.NoError(t, err)
	assert.Contains(t, output, "test1")
}
