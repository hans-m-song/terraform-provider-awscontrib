package conns

import (
	"context"
	"errors"
	"sync"
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

func TestClientCachesConnectClient(t *testing.T) {
	client, err := newWithLoader(context.Background(), Config{}, func(context.Context, ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		return aws.Config{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected configuration error: %v", err)
	}

	first := client.Connect()
	second := client.Connect()
	if first != second {
		t.Fatal("expected Connect to return the cached client")
	}
	if client.connectScheduler == nil {
		t.Fatal("expected cached Connect client to have a scheduler")
	}
}

func TestClientCachesConnectClientConcurrently(t *testing.T) {
	client, err := newWithLoader(context.Background(), Config{}, func(context.Context, ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		return aws.Config{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected configuration error: %v", err)
	}

	const callers = 32
	clients := make(chan interface{}, callers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer waitGroup.Done()
			clients <- client.Connect()
		}()
	}
	waitGroup.Wait()
	close(clients)

	var first interface{}
	for connected := range clients {
		if first == nil {
			first = connected
			continue
		}
		if first != connected {
			t.Fatal("expected concurrent Connect calls to return one client")
		}
	}
}
