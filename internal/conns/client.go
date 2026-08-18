package conns

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
)

type Client struct {
	awsConfig aws.Config
}

func (c *Client) Connect() *awsconnect.Client {
	return awsconnect.NewFromConfig(c.awsConfig)
}
