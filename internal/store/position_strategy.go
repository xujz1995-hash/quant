package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"ai_quant/internal/domain"
)

// InsertPositionStrategy 保存建仓策略
func (r *SQLiteRepository) InsertPositionStrategy(ctx context.Context, strategy domain.PositionStrategy) error {
	batchesJSON, err := json.Marshal(strategy.Batches)
	if err != nil {
		return fmt.Errorf("序列化批次数据: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO position_strategies (
			id, cycle_id, signal_id, pair, side, strategy,
			total_amount, entry_levels, batches,
			take_profit_percent, stop_loss_percent, reason, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		strategy.ID,
		strategy.CycleID,
		strategy.SignalID,
		strategy.Pair,
		strategy.Side,
		strategy.Strategy,
		strategy.TotalAmount,
		strategy.EntryLevels,
		string(batchesJSON),
		strategy.TakeProfitPercent,
		strategy.StopLossPercent,
		strategy.Reason,
		strategy.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("插入建仓策略: %w", err)
	}
	return nil
}

// GetPositionStrategy 获取建仓策略
func (r *SQLiteRepository) GetPositionStrategy(ctx context.Context, cycleID string) (*domain.PositionStrategy, error) {
	var strategy domain.PositionStrategy
	var batchesJSON string

	err := r.db.QueryRowContext(ctx, `
		SELECT id, cycle_id, signal_id, pair, side, strategy,
			   total_amount, entry_levels, batches,
			   take_profit_percent, stop_loss_percent, reason, created_at
		FROM position_strategies
		WHERE cycle_id = ?
	`, cycleID).Scan(
		&strategy.ID,
		&strategy.CycleID,
		&strategy.SignalID,
		&strategy.Pair,
		&strategy.Side,
		&strategy.Strategy,
		&strategy.TotalAmount,
		&strategy.EntryLevels,
		&batchesJSON,
		&strategy.TakeProfitPercent,
		&strategy.StopLossPercent,
		&strategy.Reason,
		&strategy.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询建仓策略: %w", err)
	}

	// 反序列化批次数据
	if err := json.Unmarshal([]byte(batchesJSON), &strategy.Batches); err != nil {
		return nil, fmt.Errorf("反序列化批次数据: %w", err)
	}

	return &strategy, nil
}
