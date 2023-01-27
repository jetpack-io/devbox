// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.
package boxcli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.jetpack.io/devbox/internal/testframework"
)

func TestRun(t *testing.T) {
	devboxJson := `
	{
		"packages": [],
		"shell": {
		  "scripts": {
			"test1": "echo test1"
		  },
		  "init_hook": null
		},
		"nixpkgs": {
		  "commit": "af9e00071d0971eb292fd5abef334e66eda3cb69"
		}
	}`
	td := testframework.Open()
	defer td.Close()
	err := td.SetDevboxJson(devboxJson)
	assert.NoError(t, err)
	_, err = td.RunCommand(RunCmd(), "test1")
	assert.NoError(t, err)
}

func TestRunCommand(t *testing.T) {
	devboxJson := `
	{
		"packages": [],
		"shell": {
		  "scripts": {
			"test1": "echo test1"
		  },
		  "init_hook": null
		},
		"nixpkgs": {
		  "commit": "af9e00071d0971eb292fd5abef334e66eda3cb69"
		}
	}`
	td := testframework.Open()
	defer td.Close()
	err := td.SetDevboxJson(devboxJson)
	assert.NoError(t, err)
	td.SetEnv("DEVBOX_FEATURE_STRICT_RUN", "1")
	o, err := td.RunCommand(RunCmd(), "ls > test.txt")
	t.Log(o)
	assert.NoError(t, err)
	assert.FileExists(t, "test.txt")

	_, err = td.RunCommand(RunCmd(), "rm test.txt")
	assert.NoError(t, err)
	assert.NoFileExists(t, "test.txt")
}
