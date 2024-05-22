// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package s3

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/pullbox/tar"
	"go.jetpack.io/devbox/internal/ux"
)

var ErrProfileNotFound = errors.New("profile not found")

func PullToTmp(
	ctx context.Context,
	creds *devopt.Credentials,
	profile string,
) (string, error) {
	config, err := assumeRole(ctx, creds)
	if err != nil {
		return "", err
	}

	// TODO(landau), before pulling, ensure that the profile exists in the cloud
	s3Client := manager.NewDownloader(s3.NewFromConfig(*config))
	buf := manager.WriteAtBuffer{}

	ux.Finfo(
		os.Stderr,
		"Logged in as %s, pulling from jetify cloud (profile: %s)\n",
		creds.Email,
		profile,
	)

	if _, err = s3Client.Download(
		ctx,
		&buf,
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key: aws.String(
				fmt.Sprintf(
					"profiles/%s/%s.tar.gz",
					creds.Sub,
					profile,
				),
			),
		},
		// TODO, we can use an s3 list objects to make this more accurate
	); err != nil && strings.Contains(err.Error(), "AccessDenied") {
		return "", ErrProfileNotFound
	} else if err != nil {
		return "", errors.WithStack(err)
	}

	dir, err := tar.Extract(buf.Bytes())
	if err != nil {
		return "", err
	}

	ux.Fsuccess(
		os.Stderr,
		"Profile successfully pulled (profile: %s)\n",
		profile,
	)

	return dir, nil
}
