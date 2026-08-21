package conns

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
)

type Client struct {
	awsConfig aws.Config

	connectOnce      sync.Once
	connectClient    *awsconnect.Client
	connectScheduler *connectScheduler
}

func (c *Client) Connect() *awsconnect.Client {
	c.connectOnce.Do(func() {
		if c.connectScheduler == nil {
			c.connectScheduler = newConnectScheduler()
		}

		c.connectClient = awsconnect.NewFromConfig(c.awsConfig, func(options *awsconnect.Options) {
			options.APIOptions = append(options.APIOptions, connectSchedulingAPIOption(c.connectScheduler))
		})
	})

	return c.connectClient
}
