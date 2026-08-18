package conns

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

func TestNewAppliesProfileAndRegion(t *testing.T) {
	var options awsconfig.LoadOptions
	loader := func(_ context.Context, optionFunctions ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		for _, optionFunction := range optionFunctions {
			if err := optionFunction(&options); err != nil {
				return aws.Config{}, err
			}
		}
		return aws.Config{Region: options.Region}, nil
	}

	client, err := newWithLoader(context.Background(), Config{Profile: "engineering", Region: "ap-southeast-2"}, loader)
	if err != nil {
		t.Fatalf("unexpected configuration error: %v", err)
	}
	if client == nil {
		t.Fatal("expected configured client")
	}
	if options.SharedConfigProfile != "engineering" {
		t.Fatalf("expected profile engineering, got %q", options.SharedConfigProfile)
	}
	if options.Region != "ap-southeast-2" {
		t.Fatalf("expected region ap-southeast-2, got %q", options.Region)
	}
}

func TestNewPropagatesConfigurationError(t *testing.T) {
	expected := errors.New("configuration failed")
	loader := func(context.Context, ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		return aws.Config{}, expected
	}

	client, err := newWithLoader(context.Background(), Config{}, loader)
	if !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
	if client != nil {
		t.Fatal("expected nil client after configuration failure")
	}
}
