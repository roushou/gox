# Gox Client SDK (`pkg/client`)

Typed helper wrappers for calling Gox MCP tools through `github.com/modelcontextprotocol/go-sdk`.

## Quick Usage

```go
client := client.New(session)

result, err := client.CallAction(ctx, "core.ping", map[string]any{}, client.CallOptions{
	Actor:         "builder-bot",
	CorrelationID: "corr-123",
})
if err != nil {
	return err
}
fmt.Println(result.RunID)
```

## Retry Helper

```go
result, err := client.CallActionWithRetry(
	ctx,
	"core.ping",
	map[string]any{},
	client.CallOptions{},
	client.RetryOptions{
		MaxAttempts:    3,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
	},
)
```

Retry behavior:
- retries only retryable failures (`error.data.retryable=true` or internal errors)
- exponential backoff (`InitialBackoff`, doubled up to `MaxBackoff`)

## Workflow Paging Helper

```go
runs, err := client.WorkflowListAll(ctx, client.WorkflowListRequest{
	Status: "succeeded",
	Limit:  100,
})
```

`WorkflowListAll` follows `nextCursor` until completion.
