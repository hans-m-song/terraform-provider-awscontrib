package conns

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

type Config struct {
	Profile string
	Region  string
}

type configLoader func(context.Context, ...func(*awsconfig.LoadOptions) error) (aws.Config, error)

func New(ctx context.Context, configured Config) (*Client, error) {
	return newWithLoader(ctx, configured, awsconfig.LoadDefaultConfig)
}

func newWithLoader(ctx context.Context, configured Config, loader configLoader) (*Client, error) {
	options := make([]func(*awsconfig.LoadOptions) error, 0, 2)
	if configured.Profile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(configured.Profile))
	}
	if configured.Region != "" {
		options = append(options, awsconfig.WithRegion(configured.Region))
	}

	loaded, err := loader(ctx, options...)
	if err != nil {
		return nil, err
	}

	return &Client{awsConfig: loaded}, nil
}
