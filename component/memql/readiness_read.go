package memql

import (
	"context"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
)

func (e *MemQLEngine) evaluateModuleReadinessExpression(ctx context.Context) ([]memorynodes.MemoryNode, error) {
	return nil, nil
}

func (e *MemQLEngine) evaluateReadinessRecomputeExpression(ctx context.Context) ([]memorynodes.MemoryNode, error) {
	return nil, nil
}
