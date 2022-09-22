// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

// Next + Nix = Nixt
// Next generation nix engine written in go
package nixt

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/cavaliergopher/grab/v3"
	"github.com/nix-community/go-nix/pkg/nar"
	"github.com/ulikunitz/xz"
)

type Nixt struct {
	cacheURL  string
	storePath string
	binPath   string
}

func New() *Nixt {
	return &Nixt{
		cacheURL:  "https://cache.nixos.org",
		storePath: "/opt/store",
		binPath:   "./bin",
	}
}

func (n *Nixt) Install(pkgs ...string) {
	// Hard-code installation path and path to download from for now.
	// Real implementation needs to determine these based on the package names.
	basePath := "/opt/store/yx99qh8pqwaqkb1n3dv7w2nf42mykkmh-hello-2.12.1"
	narURL := "https://cache.nixos.org/nar/1v7834r3k46s5pjnmi00nkf4wxp6pgyypjwysv8wqv5i663wncpm.nar.xz"

	// Download NAR file (using grab to get resumable downloads!)
	resp, err := grab.Get(n.storePath, narURL)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Download saved to", resp.Filename)

	f, err := os.Open(resp.Filename)
	if err != nil {
		log.Fatal(err)
	}

	// Extract using .xz
	xzr, err := xz.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}

	// Extract using NAR (Nix Archive)
	nr, err := nar.NewReader(xzr)
	if err != nil {
		log.Fatal(err)
	}

	for {
		hdr, err := nr.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatal(err)
		}

		path := filepath.Join(basePath, hdr.Path)

		// TODO: make atomic by first using tmp directory
		switch hdr.Type {
		case nar.TypeDirectory:
			fmt.Printf("Extracting Directory: %s\n", path)
			os.MkdirAll(path, 0755) // Should we use 0555 like nix instead?
		case nar.TypeSymlink:
			// TODO: make sure symlinks are relative (so we can still "mv" the dir)
			// If they are absolute turn to relative, otherwise use as is.
			fmt.Printf("Extracting Symlink: %s\n", path)
			os.Symlink(hdr.LinkTarget, path)
		case nar.TypeRegular:
			fmt.Printf("Extracting File: %s\n", path)
			f, err := os.Create(path)
			defer func() {
				_ = f.Close()
			}()

			if err != nil {
				log.Fatal(err)
			}

			_, err = io.Copy(f, nr) // TODO: check written bytes matches header
			if err != nil {
				log.Fatal(err)
			}

			if hdr.Executable {
				err = f.Chmod(0755) // Should we do 0555 instead?
				if err != nil {
					log.Fatal(err)
				}
			} else {
				err = f.Chmod(0644) // Should we do 0444 instead?
				if err != nil {
					log.Fatal(err)
				}
			}

			err = f.Sync()
			if err != nil {
				log.Fatal(err)
			}

		default:
			log.Fatalf("Unrecognized NAR header type: %s\n", hdr.Type)
		}
	}

	// TODO: Create symlinks from a place that would be in the path to the installed binaries.
	// For example, add symlinks from ~/bin to /opt/store/.../bin/<bin_name>
}

// func (n *Nixt) Resolve(pkgs ...string) []string {
// 	hashes := []string{}
// 	for _, pkg := range pkgs {
// 		fmt.Printf("Looking up: %s", pkg)
// 		hashes = append(hashes, "yx99qh8pqwaqkb1n3dv7w2nf42mykkmh")
// 	}
// 	return hashes
// }

// type NarInfo struct {
// 	Exists bool
// 	URL    string
// }

// func (n *Nixt) FetchNarInfo(hashes ...string) []NarInfo {
// 	infos := []NarInfo{}
// 	for _, hash := range hashes {
// 		info := n.fetchNarInfo(hash)
// 		infos = append(infos, info)
// 	}
// 	return infos
// }

// func (n *Nixt) fetchNarInfo(hash string) NarInfo {
// 	return NarInfo{}
// }
