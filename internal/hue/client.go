package hue

import (
	"github.com/dokzlo13/lightd/internal/hue/bridge"
	"time"
)

type Client = bridge.Client

func NewClient(address, token string, timeout time.Duration) *Client {
	return bridge.New(address, token, timeout)
}
