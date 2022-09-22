// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"go.jetpack.io/devbox/nixt"
)

func main() {
	fmt.Println("Running nixt")
	nx := nixt.New()
	nx.Install(os.Args[1:]...)
}
