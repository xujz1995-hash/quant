package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai_quant/internal/domain"

	_ "modernc.org/sqlite"
)

type Repository interface {
	Init(ctx context.Context) error
	Close() error
	CreateCycle(ctx context.Context, cycle domain.Cycle) error
	UpdateCycleStatus(ctx context.Context, cycleID string, status domain.CycleStatus, errMsg string) error
	InsertSignal(ctx context.Context, signal domain.Signal) error
	InsertRiskDecision(ctx context.Context, decision domain.RiskDecision) error
	InsertOrder(ctx context.Context, order domain.Order) error
	InsertCycleLog(ctx context.Context, log domain.CycleLog) error
	GetCycleReport(ctx context.Context, cycleID string) (domain.CycleReport, error)
	DeleteCycle(ctx context.Context, cycleID string) error
	ListPositions(ctx context.Context, limit int) ([]domain.PositionView, error)
	ListCycles(ctx context.Context, page, pageSize int) ([]domain.CycleSummary, error)
	CountCycles(ctx context.Context) (int, error)

	// Holdings 持仓管理
	UpsertHolding(ctx context.Context, h domain.Holding) error
	ListHoldings(ctx context.Context) ([]domain.Holding, error)
	AggregateHoldingsFromOrders(ctx context.Context) ([]domain.Holding, error)

	// Position Strategy 建仓策略管理
	InsertPositionStrategy(ctx context.Context, strategy domain.PositionStrategy) error
	GetPositionStrategy(ctx context.Context, cycleID string) (*domain.PositionStrategy, error)

	// 数据管理
	ResetAllData(ctx context.Context) error
	OrderExistsByExchangeID(ctx context.Context, exchangeOrderID string) (bool, error)
}

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(dsn string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return &SQLiteRepository{db: db}, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

func (r *SQLiteRepository) Init(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS cycles (
			id TEXT PRIMARY KEY,
			pair TEXT NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS signals (
			id TEXT PRIMARY KEY,
			cycle_id TEXT NOT NULL,
			pair TEXT NOT NULL,
			side TEXT NOT NULL,
			confidence REAL NOT NULL,
			reason TEXT NOT NULL,
			ttl_seconds INTEGER NOT NULL,
			created_at TIMESTAMP NOT NULL,
			FOREIGN KEY (cycle_id) REFERENCES cycles(id)
		);`,
		`CREATE TABLE IF NOT EXISTS risk_checks (
			id TEXT PRIMARY KEY,
			cycle_id TEXT NOT NULL,
			signal_id TEXT NOT NULL,
			approved INTEGER NOT NULL,
			reject_reason TEXT,
			max_stake_usdt REAL NOT NULL,
			created_at TIMESTAMP NOT NULL,
			FOREIGN KEY (cycle_id) REFERENCES cycles(id),
			FOREIGN KEY (signal_id) REFERENCES signals(id)
		);`,
		`CREATE TABLE IF NOT EXISTS orders (
			id TEXT PRIMARY KEY,
			cycle_id TEXT NOT NULL,
			signal_id TEXT NOT NULL,
			client_order_id TEXT NOT NULL UNIQUE,
			pair TEXT NOT NULL,
			side TEXT NOT NULL,
			stake_usdt REAL NOT NULL,
			status TEXT NOT NULL,
			exchange_order_id TEXT,
			filled_price REAL,
			raw_response TEXT,
			created_at TIMESTAMP NOT NULL,
			FOREIGN KEY (cycle_id) REFERENCES cycles(id),
			FOREIGN KEY (signal_id) REFERENCES signals(id)
		);`,
		`CREATE TABLE IF NOT EXISTS cycle_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cycle_id TEXT NOT NULL,
			stage TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			FOREIGN KEY (cycle_id) REFERENCES cycles(id)
		);`,
		`CREATE TABLE IF NOT EXISTS holdings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pair TEXT NOT NULL UNIQUE,
			symbol TEXT NOT NULL,
			quantity REAL NOT NULL DEFAULT 0,
			avg_price REAL NOT NULL DEFAULT 0,
			total_cost REAL NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'local',
			updated_at TIMESTAMP NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS position_strategies (
			id TEXT PRIMARY KEY,
			cycle_id TEXT NOT NULL,
			signal_id TEXT NOT NULL,
			pair TEXT NOT NULL,
			side TEXT NOT NULL,
			strategy TEXT NOT NULL,
			total_amount REAL NOT NULL,
			entry_levels INTEGER NOT NULL,
			batches TEXT NOT NULL,
			take_profit_percent REAL NOT NULL,
			stop_loss_percent REAL NOT NULL,
			reason TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			FOREIGN KEY (cycle_id) REFERENCES cycles(id),
			FOREIGN KEY (signal_id) REFERENCES signals(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_signals_cycle_id ON signals(cycle_id);`,
		`CREATE INDEX IF NOT EXISTS idx_position_strategies_cycle_id ON position_strategies(cycle_id);`,
		`CREATE INDEX IF NOT EXISTS idx_risk_cycle_id ON risk_checks(cycle_id);`,
		`CREATE INDEX IF NOT EXISTS idx_orders_cycle_id ON orders(cycle_id);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_cycle_id ON cycle_logs(cycle_id);`,
		// 兼容旧库：添加 filled_qty 列（已存在则忽略）
		`ALTER TABLE orders ADD COLUMN filled_qty REAL;`,
		// 兼容旧库：添加 thinking 列存储 AI 思维链
		`ALTER TABLE signals ADD COLUMN thinking TEXT;`,
		// 兼容旧库：添加 token 用量列
		`ALTER TABLE signals ADD COLUMN prompt_tokens INTEGER DEFAULT 0;`,
		`ALTER TABLE signals ADD COLUMN completion_tokens INTEGER DEFAULT 0;`,
		`ALTER TABLE signals ADD COLUMN total_tokens INTEGER DEFAULT 0;`,
		// 兼容旧库：添加 leverage 列（合约杠杆倍数）
		`ALTER TABLE orders ADD COLUMN leverage INTEGER DEFAULT 0;`,
		// 兼容旧库：添加 model_name 列（记录使用的模型）
		`ALTER TABLE signals ADD COLUMN model_name TEXT DEFAULT '';`,
	}

	for _, stmt := range stmts {
		_, err := r.db.ExecContext(ctx, stmt)
		if err != nil {
			// ALTER TABLE ADD COLUMN 在列已存在时会报错，忽略此类错误
			if isAlterTableDuplicate(err) {
				continue
			}
			return fmt.Errorf("migrate sqlite: %w", err)
		}
	}

	return nil
}

func (r *SQLiteRepository) CreateCycle(ctx context.Context, cycle domain.Cycle) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO cycles (id, pair, status, error_message, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		cycle.ID,
		cycle.Pair,
		string(cycle.Status),
		nullableString(cycle.ErrorMessage),
		cycle.CreatedAt.UTC(),
		cycle.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert cycle: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) UpdateCycleStatus(ctx context.Context, cycleID string, status domain.CycleStatus, errMsg string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE cycles SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		string(status),
		nullableString(errMsg),
		time.Now().UTC(),
		cycleID,
	)
	if err != nil {
		return fmt.Errorf("update cycle status: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) InsertSignal(ctx context.Context, signal domain.Signal) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO signals (id, cycle_id, pair, side, confidence, reason, thinking, prompt_tokens, completion_tokens, total_tokens, model_name, ttl_seconds, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		signal.ID,
		signal.CycleID,
		signal.Pair,
		string(signal.Side),
		signal.Confidence,
		signal.Reason,
		nullableString(signal.Thinking),
		signal.PromptTokens,
		signal.CompletionTokens,
		signal.TotalTokens,
		signal.ModelName,
		signal.TTLSeconds,
		signal.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert signal: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) InsertRiskDecision(ctx context.Context, decision domain.RiskDecision) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO risk_checks (id, cycle_id, signal_id, approved, reject_reason, max_stake_usdt, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		decision.ID,
		decision.CycleID,
		decision.SignalID,
		boolToInt(decision.Approved),
		nullableString(decision.RejectReason),
		decision.MaxStakeUSDT,
		decision.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert risk decision: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) InsertOrder(ctx context.Context, order domain.Order) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO orders (id, cycle_id, signal_id, client_order_id, pair, side, stake_usdt, leverage, status, exchange_order_id, filled_price, filled_qty, raw_response, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.ID,
		order.CycleID,
		order.SignalID,
		order.ClientOrderID,
		order.Pair,
		string(order.Side),
		order.StakeUSDT,
		order.Leverage,
		order.Status,
		nullableString(order.ExchangeOrderID),
		nullableFloat(order.FilledPrice),
		nullableFloat(order.FilledQuantity),
		nullableString(order.RawResponse),
		order.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) InsertCycleLog(ctx context.Context, log domain.CycleLog) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO cycle_logs (cycle_id, stage, message, created_at) VALUES (?, ?, ?, ?)`,
		log.CycleID,
		log.Stage,
		log.Message,
		log.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert cycle log: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetCycleReport(ctx context.Context, cycleID string) (domain.CycleReport, error) {
	var report domain.CycleReport

	cycle, err := r.getCycle(ctx, cycleID)
	if err != nil {
		return report, err
	}
	report.Cycle = cycle

	signal, err := r.getSignal(ctx, cycleID)
	if err != nil {
		return report, err
	}
	if signal != nil {
		report.Signal = signal
	}

	risk, err := r.getRisk(ctx, cycleID)
	if err != nil {
		return report, err
	}
	if risk != nil {
		report.Risk = risk
	}

	order, err := r.getOrder(ctx, cycleID)
	if err != nil {
		return report, err
	}
	if order != nil {
		report.Order = order
	}

	// 获取建仓策略
	posStrategy, err := r.GetPositionStrategy(ctx, cycleID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return report, err
	}
	if posStrategy != nil {
		report.PositionStrategy = posStrategy
	}

	logs, err := r.getLogs(ctx, cycleID)
	if err != nil {
		return report, err
	}
	report.Logs = logs

	return report, nil
}

func (r *SQLiteRepository) getCycle(ctx context.Context, cycleID string) (domain.Cycle, error) {
	var cycle domain.Cycle
	var status string
	var errMsg sql.NullString

	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, pair, status, error_message, created_at, updated_at FROM cycles WHERE id = ?`,
		cycleID,
	).Scan(&cycle.ID, &cycle.Pair, &status, &errMsg, &cycle.CreatedAt, &cycle.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cycle, fmt.Errorf("cycle %s not found", cycleID)
		}
		return cycle, fmt.Errorf("query cycle: %w", err)
	}

	cycle.Status = domain.CycleStatus(status)
	if errMsg.Valid {
		cycle.ErrorMessage = errMsg.String
	}

	return cycle, nil
}

func (r *SQLiteRepository) getSignal(ctx context.Context, cycleID string) (*domain.Signal, error) {
	var signal domain.Signal
	var side string
	var thinking, modelName sql.NullString
	var promptTok, completionTok, totalTok sql.NullInt64

	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, cycle_id, pair, side, confidence, reason, COALESCE(thinking, ''),
		        COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0), COALESCE(total_tokens, 0),
		        COALESCE(model_name, ''), ttl_seconds, created_at
		 FROM signals WHERE cycle_id = ? ORDER BY created_at DESC LIMIT 1`,
		cycleID,
	).Scan(&signal.ID, &signal.CycleID, &signal.Pair, &side, &signal.Confidence, &signal.Reason, &thinking,
		&promptTok, &completionTok, &totalTok, &modelName,
		&signal.TTLSeconds, &signal.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query signal: %w", err)
	}

	signal.Side = domain.Side(side)
	if thinking.Valid {
		signal.Thinking = thinking.String
	}
	if promptTok.Valid {
		signal.PromptTokens = int(promptTok.Int64)
	}
	if completionTok.Valid {
		signal.CompletionTokens = int(completionTok.Int64)
	}
	if totalTok.Valid {
		signal.TotalTokens = int(totalTok.Int64)
	}
	if modelName.Valid {
		signal.ModelName = modelName.String
	}
	return &signal, nil
}

func (r *SQLiteRepository) getRisk(ctx context.Context, cycleID string) (*domain.RiskDecision, error) {
	var risk domain.RiskDecision
	var approved int
	var rejectReason sql.NullString

	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, cycle_id, signal_id, approved, reject_reason, max_stake_usdt, created_at
		 FROM risk_checks WHERE cycle_id = ? ORDER BY created_at DESC LIMIT 1`,
		cycleID,
	).Scan(&risk.ID, &risk.CycleID, &risk.SignalID, &approved, &rejectReason, &risk.MaxStakeUSDT, &risk.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query risk: %w", err)
	}

	risk.Approved = approved == 1
	if rejectReason.Valid {
		risk.RejectReason = rejectReason.String
	}
	return &risk, nil
}

func (r *SQLiteRepository) getOrder(ctx context.Context, cycleID string) (*domain.Order, error) {
	var order domain.Order
	var side string
	var exchangeOrderID sql.NullString
	var filledPrice sql.NullFloat64
	var rawResp sql.NullString

	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, cycle_id, signal_id, client_order_id, pair, side, stake_usdt, status, exchange_order_id, filled_price, raw_response, created_at
		 FROM orders WHERE cycle_id = ? ORDER BY created_at DESC LIMIT 1`,
		cycleID,
	).Scan(
		&order.ID,
		&order.CycleID,
		&order.SignalID,
		&order.ClientOrderID,
		&order.Pair,
		&side,
		&order.StakeUSDT,
		&order.Status,
		&exchangeOrderID,
		&filledPrice,
		&rawResp,
		&order.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query order: %w", err)
	}

	order.Side = domain.Side(side)
	if exchangeOrderID.Valid {
		order.ExchangeOrderID = exchangeOrderID.String
	}
	if filledPrice.Valid {
		order.FilledPrice = filledPrice.Float64
	}
	if rawResp.Valid {
		order.RawResponse = rawResp.String
	}

	return &order, nil
}

// DeleteCycle 删除周期及其关联的所有数据（信号、风控、订单、日志、建仓策略）
func (r *SQLiteRepository) DeleteCycle(ctx context.Context, cycleID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务: %w", err)
	}
	defer tx.Rollback()

	// 删除关联数据（按外键依赖顺序）
	tables := []string{
		"cycle_logs",
		"orders",
		"risk_checks",
		"position_strategies",
		"signals",
		"cycles",
	}

	for _, table := range tables {
		_, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE cycle_id = ?", table), cycleID)
		if err != nil {
			return fmt.Errorf("删除 %s: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}

	return nil
}

func (r *SQLiteRepository) getLogs(ctx context.Context, cycleID string) ([]domain.CycleLog, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, cycle_id, stage, message, created_at FROM cycle_logs WHERE cycle_id = ? ORDER BY id ASC`,
		cycleID,
	)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	logs := make([]domain.CycleLog, 0)
	for rows.Next() {
		var log domain.CycleLog
		if scanErr := rows.Scan(&log.ID, &log.CycleID, &log.Stage, &log.Message, &log.CreatedAt); scanErr != nil {
			return nil, fmt.Errorf("scan logs: %w", scanErr)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate logs: %w", err)
	}

	return logs, nil
}

func (r *SQLiteRepository) ListPositions(ctx context.Context, limit int) ([]domain.PositionView, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			o.id, o.cycle_id, o.pair, o.side, o.stake_usdt, o.filled_price, o.filled_qty, o.status,
			COALESCE(o.exchange_order_id, ''), s.reason, s.confidence, c.status, o.created_at
		FROM orders o
		JOIN signals s ON s.cycle_id = o.cycle_id
		JOIN cycles c ON c.id = o.cycle_id
		ORDER BY o.created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("查询仓位列表: %w", err)
	}
	defer rows.Close()

	positions := make([]domain.PositionView, 0)
	for rows.Next() {
		var p domain.PositionView
		var side, cycleStatus string
		var filledPrice, filledQty sql.NullFloat64
		if err := rows.Scan(
			&p.OrderID, &p.CycleID, &p.Pair, &side, &p.StakeUSDT, &filledPrice, &filledQty, &p.Status,
			&p.ExchangeOrderID, &p.SignalReason, &p.Confidence, &cycleStatus, &p.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描仓位记录: %w", err)
		}
		p.Side = domain.Side(side)
		p.CycleStatus = cycleStatus
		if filledPrice.Valid {
			p.FilledPrice = filledPrice.Float64
		}
		if filledQty.Valid {
			p.FilledQuantity = filledQty.Float64
		} else if p.FilledPrice > 0 && p.StakeUSDT > 0 {
			// 旧数据兜底计算
			p.FilledQuantity = p.StakeUSDT / p.FilledPrice
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历仓位记录: %w", err)
	}
	return positions, nil
}

// ==================== 周期列表（分页） ====================

// CountCycles 统计周期总数
func (r *SQLiteRepository) CountCycles(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cycles").Scan(&count)
	return count, err
}

// ListCycles 分页查询周期摘要（含信号、风控、订单关键字段）
func (r *SQLiteRepository) ListCycles(ctx context.Context, page, pageSize int) ([]domain.CycleSummary, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 15
	}
	offset := (page - 1) * pageSize

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			c.id, c.pair, c.status, COALESCE(c.error_message, ''),
			COALESCE(s.side, ''),
			COALESCE(s.confidence, 0),
			COALESCE(s.reason, ''),
			COALESCE(s.total_tokens, 0),
			COALESCE(s.model_name, ''),
			r.approved,
			COALESCE(r.reject_reason, ''),
			COALESCE(o.stake_usdt, 0),
			COALESCE(o.filled_price, 0),
			COALESCE(o.status, ''),
			c.created_at
		FROM cycles c
		LEFT JOIN signals s ON s.cycle_id = c.id
		LEFT JOIN risk_checks r ON r.cycle_id = c.id
		LEFT JOIN orders o ON o.cycle_id = c.id
		ORDER BY c.created_at DESC
		LIMIT ? OFFSET ?
	`, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("查询周期列表: %w", err)
	}
	defer rows.Close()

	results := make([]domain.CycleSummary, 0, pageSize)
	for rows.Next() {
		var cs domain.CycleSummary
		var status, side, errMsg, reason, modelName, rejectReason, orderStatus string
		var riskApproved sql.NullInt64

		if err := rows.Scan(
			&cs.CycleID, &cs.Pair, &status, &errMsg,
			&side, &cs.Confidence, &reason, &cs.TotalTokens, &modelName,
			&riskApproved, &rejectReason,
			&cs.StakeUSDT, &cs.FilledPrice, &orderStatus,
			&cs.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描周期记录: %w", err)
		}

		cs.Status = domain.CycleStatus(status)
		cs.SignalSide = domain.Side(side)
		cs.SignalReason = reason
		cs.ModelName = modelName
		cs.ErrorMessage = errMsg
		cs.OrderStatus = orderStatus
		cs.RejectReason = rejectReason
		if riskApproved.Valid {
			approved := riskApproved.Int64 == 1
			cs.RiskApproved = &approved
		}

		results = append(results, cs)
	}
	return results, rows.Err()
}

// ==================== Holdings 持仓管理 ====================

// UpsertHolding 插入或更新持仓（按 pair 唯一键）
func (r *SQLiteRepository) UpsertHolding(ctx context.Context, h domain.Holding) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO holdings (pair, symbol, quantity, avg_price, total_cost, source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pair) DO UPDATE SET
			quantity   = excluded.quantity,
			avg_price  = excluded.avg_price,
			total_cost = excluded.total_cost,
			source     = excluded.source,
			updated_at = excluded.updated_at
	`, h.Pair, h.Symbol, h.Quantity, h.AvgPrice, h.TotalCost, h.Source, h.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("upsert holding: %w", err)
	}
	return nil
}

// ListHoldings 获取所有持仓记录
func (r *SQLiteRepository) ListHoldings(ctx context.Context) ([]domain.Holding, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, pair, symbol, quantity, avg_price, total_cost, source, updated_at
		FROM holdings
		WHERE quantity > 0
		ORDER BY total_cost DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("查询持仓: %w", err)
	}
	defer rows.Close()

	holdings := make([]domain.Holding, 0)
	for rows.Next() {
		var h domain.Holding
		if err := rows.Scan(&h.ID, &h.Pair, &h.Symbol, &h.Quantity, &h.AvgPrice, &h.TotalCost, &h.Source, &h.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描持仓记录: %w", err)
		}
		holdings = append(holdings, h)
	}
	return holdings, rows.Err()
}

// AggregateHoldingsFromOrders 从历史订单聚合计算各币对当前持仓
func (r *SQLiteRepository) AggregateHoldingsFromOrders(ctx context.Context) ([]domain.Holding, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT pair, side, filled_price, filled_qty
		FROM orders
		WHERE status IN ('filled', 'simulated_filled')
		  AND filled_qty > 0 AND filled_price > 0
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("查询订单聚合: %w", err)
	}
	defer rows.Close()

	// 按币对聚合：买入增加持仓，卖出减少持仓
	type acc struct {
		qty       float64
		totalCost float64
	}
	pairMap := make(map[string]*acc)

	for rows.Next() {
		var pair, side string
		var price, qty float64
		if err := rows.Scan(&pair, &side, &price, &qty); err != nil {
			return nil, fmt.Errorf("扫描订单: %w", err)
		}
		a, ok := pairMap[pair]
		if !ok {
			a = &acc{}
			pairMap[pair] = a
		}
		if side == "long" {
			// 买入：增加持仓和成本
			a.totalCost += qty * price
			a.qty += qty
		} else if side == "close" {
			// 卖出：减少持仓，按比例减少成本
			if a.qty > 0 {
				ratio := qty / a.qty
				if ratio > 1 {
					ratio = 1
				}
				a.totalCost -= a.totalCost * ratio
			}
			a.qty -= qty
			if a.qty < 0 {
				a.qty = 0
				a.totalCost = 0
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	result := make([]domain.Holding, 0, len(pairMap))
	for pair, a := range pairMap {
		if a.qty <= 0 {
			continue
		}
		symbol := strings.Split(pair, "/")[0]
		avgPrice := 0.0
		if a.qty > 0 {
			avgPrice = a.totalCost / a.qty
		}
		result = append(result, domain.Holding{
			Pair:      pair,
			Symbol:    symbol,
			Quantity:  a.qty,
			AvgPrice:  avgPrice,
			TotalCost: a.totalCost,
			Source:    "local",
			UpdatedAt: now,
		})
	}
	return result, nil
}

// ResetAllData 清空所有业务数据（保留表结构）
func (r *SQLiteRepository) ResetAllData(ctx context.Context) error {
	tables := []string{"holdings", "cycle_logs", "orders", "risk_checks", "signals", "cycles"}
	for _, t := range tables {
		if _, err := r.db.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return fmt.Errorf("清空表 %s 失败: %w", t, err)
		}
	}
	// 重置自增 ID
	if _, err := r.db.ExecContext(ctx, "DELETE FROM sqlite_sequence"); err != nil {
		// sqlite_sequence 可能不存在，忽略
		_ = err
	}
	return nil
}

// OrderExistsByExchangeID 检查某个交易所订单 ID 是否已存在（用于去重）
func (r *SQLiteRepository) OrderExistsByExchangeID(ctx context.Context, exchangeOrderID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM orders WHERE exchange_order_id = ?", exchangeOrderID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// isAlterTableDuplicate 检查是否为 ALTER TABLE ADD COLUMN 列已存在的错误
func isAlterTableDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}
