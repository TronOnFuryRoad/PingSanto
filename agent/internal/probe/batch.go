package probe

import (
	"context"
	"time"

	"github.com/pingsantohq/agent/pkg/types"
)

func Batch(ctx context.Context, reqs []Request) ([]types.ProbeResult, error) {
	results := make([]types.ProbeResult, 0, len(reqs))
	now := time.Now().UTC()
	for _, req := range reqs {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}
		result := types.ProbeResult{
			MonitorID:       req.MonitorID,
			Timestamp:       now,
			Proto:           req.Protocol,
			Success:         true,
			RTTMilliseconds: 0,
		}
		if len(req.Targets) > 0 {
			result.IP = req.Targets[0]
		}
		results = append(results, result)
	}
	return results, nil
}
