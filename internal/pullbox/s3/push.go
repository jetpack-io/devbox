// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package s3

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/pullbox/tar"
	"go.jetpack.io/devbox/internal/ux"
)

func Push(
	ctx context.Context,
	creds *devopt.Credentials,
	dir, profile string,
) error {
	archivePath, err := tar.Compress(dir)
	if err != nil {
		return err
	}

	config, err := assumeRole(ctx, creds)
	if err != nil {
		return err
	}

	s3Client := manager.NewUploader(s3.NewFromConfig(*config))
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = s3Client.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key: aws.String(
			fmt.Sprintf(
				"profiles/%s/%s.tar.gz",
				creds.Sub,
				profile,
			),
		),
		Body: io.Reader(file),
	})
	if err != nil {
		return err
	}

	ux.Fsuccess(
		os.Stderr,
		"Profile successfully pushed (profile: %s)\n",
		profile,
	)

	return nil
}
