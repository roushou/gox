package roblox

import "context"

type Transport interface {
	Call(ctx context.Context, req Request) (Response, error)
	Ping(ctx context.Context) error
	Close() error
}
