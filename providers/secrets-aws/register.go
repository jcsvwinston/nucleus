// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package awssm resolves JWT signing material from AWS Secrets Manager,
// published as its own module so the framework does not link the AWS SDK
// for deployments that keep their secrets in environment variables.
//
// Import it for its side effect and references of the form
// "aws-sm:prod/jwt#key" become resolvable:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/secrets-aws"
package awssm

import (
	"context"

	"github.com/jcsvwinston/nucleus/pkg/auth/secrets"
)

// schemeAWSSM is the reference prefix this resolver owns.
const schemeAWSSM = "aws-sm:"

func init() {
	if err := secrets.RegisterResolver(schemeAWSSM, func(ctx context.Context) (secrets.Resolver, error) {
		return NewAWSSecretsManagerResolver(ctx)
	}); err != nil {
		panic("secrets-aws: registering resolver: " + err.Error())
	}
}
