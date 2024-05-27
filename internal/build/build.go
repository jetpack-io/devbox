// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package build

import (
	"os"
	"runtime"
	"strconv"
	"sync"

	"go.jetpack.io/devbox/internal/fileutil"
)

var forceProd, _ = strconv.ParseBool(os.Getenv("DEVBOX_PROD"))

// Variables in this file are set via ldflags.
var (
	IsDev      = Version == "0.0.0-dev" && !forceProd
	Version    = "0.0.0-dev"
	Commit     = "none"
	CommitDate = "unknown"

	// SentryDSN is injected in the build from
	// https://jetpack-io.sentry.io/settings/projects/devbox/keys/
	// It is disabled by default.
	SentryDSN = ""
	// TelemetryKey is the Segment Write Key
	// https://segment.com/docs/connections/sources/catalog/libraries/server/go/quickstart/
	// It is disabled by default.
	TelemetryKey = ""
)

// User-presentable names of operating systems supported by Devbox.
const (
	OSLinux  = "Linux"
	OSDarwin = "macOS"
	OSWSL    = "WSL"
)

var (
	osName string
	osOnce sync.Once
)

func OS() string {
	osOnce.Do(func() {
		switch runtime.GOOS {
		case "linux":
			if fileutil.Exists("/proc/sys/fs/binfmt_misc/WSLInterop") || fileutil.Exists("/run/WSL") {
				osName = OSWSL
			}
			osName = OSLinux
		case "darwin":
			osName = OSDarwin
		default:
			osName = runtime.GOOS
		}
	})
	return osName
}

func Issuer() string {
	if IsDev {
		return "https://laughing-agnesi-vzh2rap9f6.projects.oryapis.com"
	}
	return "https://accounts.jetify.com"
}

func ClientID() string {
	if IsDev {
		return "3945b320-bd31-4313-af27-846b67921acb"
	}
	return "ff3d4c9c-1ac8-42d9-bef1-f5218bb1a9f6"
}

func JetpackAPIHost() string {
	if IsDev {
		return "https://api.jetpack.dev"
	}
	return "https://api.jetpack.io"
}

func SuccessRedirect() string {
	if IsDev {
		return "https://auth.jetpack.dev/account/login/success"
	}
	return "https://auth.jetify.com/account/login/success"
}

func Audience() []string {
	return []string{"https://api.jetpack.io"}
}

func DashboardHostname() string {
	if IsDev {
		return "http://localhost:8080"
	}
	return "https://cloud.jetify.com"
}
