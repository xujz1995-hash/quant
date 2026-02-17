package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai_quant/internal/agent/execution"
	"ai_quant/internal/agent/position"
	"ai_quant/internal/agent/risk"
	"ai_quant/internal/agent/signal"
	"ai_quant/internal/domain"
	"ai_quant/internal/market"
	"ai_quant/internal/store"

	"github.com/google/uuid"
)

type Service struct {
	repo     store.Repository
	signal   signal.Agent
	risk     risk.Agent
	position position.Agent
	executor execution.Executor
}

type RunRequest struct {
	Pair      string
	Snapshot  *domain.MarketSnapshot
	Portfolio domain.PortfolioState
}

func New(repo store.Repository, signalAgent signal.Agent, riskAgent risk.Agent, positionAgent position.Agent, executor execution.Executor) *Service {
	svc := &Service{
		repo:     repo,
		signal:   signalAgent,
		risk:     riskAgent,
		position: positionAgent,
		executor: executor,
	}

	// 注入真实账户数据回调到 signal agent
	signal.SetAccountDataFunc(signalAgent, func(ctx context.Context, pair string) (float64, []market.PositionData) {
		return svc.fetchAccountDataForPrompt(ctx, pair)
	})

	// 注入交易模式信息到 signal agent
	signal.SetTradingMode(signalAgent, executor.TradingMode(), executor.Leverage())

	return svc
}

func (s *Service) RunCycle(ctx context.Context, req RunRequest) (domain.CycleResult, error) {
	cycleStart := time.Now()
	pair := strings.ToUpper(strings.TrimSpace(req.Pair))
	if pair == "" {
		pair = "BTC/USDT"
	}

	now := time.Now().UTC()
	cycle := domain.Cycle{
		ID:        uuid.NewString(),
		Pair:      pair,
		Status:    domain.CycleStatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	log.Printf("[周期:%s] ▶ 开始执行 交易对=%s", cycle.ID[:8], pair)

	if err := s.repo.CreateCycle(ctx, cycle); err != nil {
		log.Printf("[周期:%s] ✘ 创建周期失败: %v", cycle.ID[:8], err)
		return domain.CycleResult{}, err
	}

	logs := make([]domain.CycleLog, 0, 6)
	addLog := func(stage, message string) error {
		entry := domain.CycleLog{
			CycleID:   cycle.ID,
			Stage:     stage,
			Message:   message,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.repo.InsertCycleLog(ctx, entry); err != nil {
			return err
		}
		logs = append(logs, entry)
		return nil
	}

	_ = addLog("启动", "周期开始执行")

	snapshot := fallbackSnapshot(pair, req.Snapshot)
	// 如果没有外部传入行情（定时器自动触发），快速从 Binance 拉取实时价格
	if snapshot.LastPrice == 0 {
		if price, change, err := fetchQuickTicker(ctx, pair); err == nil {
			snapshot.LastPrice = price
			snapshot.Change24h = change
			log.Printf("[周期:%s] 📊 已从 Binance 获取实时行情 价格=%.6f 24h涨跌=%.2f%%", cycle.ID[:8], price, change)
		} else {
			log.Printf("[周期:%s] ⚠ 快速行情获取失败: %v（AI 会自行获取完整数据）", cycle.ID[:8], err)
		}
	}
	log.Printf("[周期:%s] 📊 行情快照 价格=%.6f 24h涨跌=%.2f%%", cycle.ID[:8], snapshot.LastPrice, snapshot.Change24h)
	_ = addLog("行情", fmt.Sprintf("价格=%.6f 24h涨跌=%.2f%%", snapshot.LastPrice, snapshot.Change24h))

	// ---- 信号生成 ----
	signalStart := time.Now()
	log.Printf("[周期:%s] 🤖 信号: 正在调用大模型分析 %s ...", cycle.ID[:8], pair)
	sig, err := s.signal.Generate(ctx, signal.Input{CycleID: cycle.ID, Pair: pair, Snapshot: snapshot})
	signalElapsed := time.Since(signalStart)
	if err != nil {
		log.Printf("[周期:%s] ✘ 信号生成失败 耗时%s: %v", cycle.ID[:8], signalElapsed, err)
		_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusFailed, err.Error())
		_ = addLog("信号", "信号生成失败: "+err.Error())
		return domain.CycleResult{}, err
	}
	log.Printf("[周期:%s] ✔ 信号: 方向=%s 置信度=%.2f 理由=%q (耗时%s)", cycle.ID[:8], sig.Side, sig.Confidence, sig.Reason, signalElapsed)

	if err := s.repo.InsertSignal(ctx, sig); err != nil {
		log.Printf("[周期:%s] ✘ 保存信号失败: %v", cycle.ID[:8], err)
		_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusFailed, err.Error())
		return domain.CycleResult{}, err
	}
	_ = addLog("信号", fmt.Sprintf("方向=%s 置信度=%.2f 理由=%s", sig.Side, sig.Confidence, sig.Reason))

	// ---- 风控评估 ----
	log.Printf("[周期:%s] 🛡️ 风控: 正在评估 ...", cycle.ID[:8])
	riskDecision, err := s.risk.Evaluate(ctx, risk.Input{CycleID: cycle.ID, Signal: sig, Portfolio: req.Portfolio})
	if err != nil {
		log.Printf("[周期:%s] ✘ 风控评估失败: %v", cycle.ID[:8], err)
		_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusFailed, err.Error())
		_ = addLog("风控", "风控评估失败: "+err.Error())
		return domain.CycleResult{}, err
	}
	if err := s.repo.InsertRiskDecision(ctx, riskDecision); err != nil {
		log.Printf("[周期:%s] ✘ 保存风控决策失败: %v", cycle.ID[:8], err)
		_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusFailed, err.Error())
		return domain.CycleResult{}, err
	}

	if !riskDecision.Approved {
		log.Printf("[周期:%s] ⚠️ 风控: 已拒绝 原因=%q", cycle.ID[:8], riskDecision.RejectReason)
		_ = addLog("风控", "已拒绝: "+riskDecision.RejectReason)
		_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusRejected, riskDecision.RejectReason)
		cycle.Status = domain.CycleStatusRejected
		cycle.ErrorMessage = riskDecision.RejectReason
		cycle.UpdatedAt = time.Now().UTC()

		log.Printf("[周期:%s] ■ 执行完毕 状态=已拒绝 总耗时=%s", cycle.ID[:8], time.Since(cycleStart))
		return domain.CycleResult{
			Cycle:  cycle,
			Signal: sig,
			Risk:   riskDecision,
			Logs:   logs,
		}, nil
	}
	log.Printf("[周期:%s] ✔ 风控: 已通过 最大仓位=%.2f USDT", cycle.ID[:8], riskDecision.MaxStakeUSDT)
	_ = addLog("风控", fmt.Sprintf("已通过 最大仓位=%.2f", riskDecision.MaxStakeUSDT))

	// ---- 建仓策略生成 ----
	log.Printf("[周期:%s] 📊 建仓策略: 正在生成 ...", cycle.ID[:8])
	posStrategy, err := s.position.Generate(ctx, position.Input{
		CycleID:      cycle.ID,
		SignalID:     sig.ID,
		Pair:         pair,
		Side:         sig.Side,
		Signal:       sig,
		MaxStakeUSDT: riskDecision.MaxStakeUSDT,
		CurrentPrice: snapshot.LastPrice,
	})
	if err != nil {
		log.Printf("[周期:%s] ✘ 建仓策略生成失败: %v", cycle.ID[:8], err)
		_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusFailed, err.Error())
		_ = addLog("建仓策略", "生成失败: "+err.Error())
		return domain.CycleResult{}, err
	}

	// 保存建仓策略
	if err := s.repo.InsertPositionStrategy(ctx, posStrategy); err != nil {
		log.Printf("[周期:%s] ✘ 保存建仓策略失败: %v", cycle.ID[:8], err)
	}

	log.Printf("[周期:%s] ✔ 建仓策略: %s 分批=%d 止盈=%.1f%% 止损=%.1f%%",
		cycle.ID[:8], posStrategy.Strategy, posStrategy.EntryLevels,
		posStrategy.TakeProfitPercent, posStrategy.StopLossPercent)
	_ = addLog("建仓策略", fmt.Sprintf("%s: %s", posStrategy.Strategy, posStrategy.Reason))

	// ---- 下单执行 ----
	// 注意：当前版本执行第一批次，后续批次需要单独实现触发逻辑
	execInput := execution.Input{
		CycleID:       cycle.ID,
		SignalID:      sig.ID,
		Pair:          pair,
		Side:          sig.Side,
		StakeUSDT:     riskDecision.MaxStakeUSDT,
		EstimatedFill: snapshot.LastPrice,
	}

	// 如果是买入且有分批策略，只执行第一批
	if sig.Side == domain.SideLong && len(posStrategy.Batches) > 0 {
		firstBatch := posStrategy.Batches[0]
		execInput.StakeUSDT = firstBatch.Amount
		log.Printf("[周期:%s] 📦 执行第1批: %.2f USDT (共%d批)", cycle.ID[:8], firstBatch.Amount, len(posStrategy.Batches))
	}

	// 买入信号：检查实际可用余额，自动调整金额避免余额不足
	if sig.Side == domain.SideLong && !s.executor.IsDryRun() {
		balances, bErr := s.executor.FetchFullBalance(ctx)
		if bErr == nil {
			for _, b := range balances {
				if b.Symbol == "USDT" {
					available := b.Free
					// 预留 1 USDT 作为手续费缓冲
					maxCanSpend := available - 1.0
					if maxCanSpend < 5 {
						log.Printf("[周期:%s] ⚠ USDT余额不足: 可用=%.2f，最少需5U，跳过本轮", cycle.ID[:8], available)
						_ = addLog("执行", fmt.Sprintf("跳过: USDT余额不足 可用=%.2f", available))
						_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusFailed, "USDT余额不足")
						return domain.CycleResult{Cycle: cycle, Signal: sig, Risk: riskDecision, Logs: logs}, nil
					}
					if execInput.StakeUSDT > maxCanSpend {
						log.Printf("[周期:%s] 💰 余额调整: 计划=%.2f 可用=%.2f → 实际下单=%.2f",
							cycle.ID[:8], execInput.StakeUSDT, available, maxCanSpend)
						execInput.StakeUSDT = maxCanSpend
					}
					break
				}
			}
		} else {
			log.Printf("[周期:%s] ⚠ 获取余额失败: %v，使用风控金额 %.2f", cycle.ID[:8], bErr, execInput.StakeUSDT)
		}
	}

	// close 信号：查询持仓数量，用币数量卖出/平仓
	if sig.Side == domain.SideClose {
		if s.executor.TradingMode() == "futures" {
			// 合约模式：通过 positionRisk API 获取持仓数量
			posAmt, pErr := s.executor.FetchPositionRisk(ctx, pair)
			if pErr == nil && posAmt > 0 {
				execInput.SellQuantity = posAmt
				log.Printf("[周期:%s] 📦 合约平仓: %s 持仓数量=%.4f", cycle.ID[:8], pair, posAmt)
			}
			// dry-run 模式查本地持仓
			if execInput.SellQuantity <= 0 {
				holdings, hErr := s.repo.ListHoldings(ctx)
				if hErr == nil {
					for _, h := range holdings {
						if strings.EqualFold(h.Pair, pair) && h.Quantity > 0 {
							execInput.SellQuantity = h.Quantity
							log.Printf("[周期:%s] 📦 合约平仓(本地): %s 数量=%.4f", cycle.ID[:8], pair, h.Quantity)
							break
						}
					}
				}
			}
		} else {
			// 现货模式
			coin := strings.Split(pair, "/")[0]

			if s.executor.IsDryRun() {
				// 模拟盘：用本地 holdings 表
				holdings, hErr := s.repo.ListHoldings(ctx)
				if hErr == nil {
					for _, h := range holdings {
						if strings.EqualFold(h.Pair, pair) && h.Quantity > 0 {
							execInput.SellQuantity = h.Quantity
							log.Printf("[周期:%s] 📦 模拟平仓: 持仓 %s 数量=%.4f", cycle.ID[:8], pair, h.Quantity)
							break
						}
					}
				}
			} else {
				// 实盘：以交易所真实余额为准（避免本地数据与实际不一致）
				balances, bErr := s.executor.FetchFullBalance(ctx)
				if bErr == nil {
					for _, b := range balances {
						if strings.EqualFold(b.Symbol, coin) && b.Free > 0 {
							execInput.SellQuantity = b.Free
							log.Printf("[周期:%s] 📦 平仓(交易所真实余额): %s 可用=%.4f", cycle.ID[:8], coin, b.Free)
							break
						}
					}
				} else {
					log.Printf("[周期:%s] ⚠ 获取交易所余额失败: %v，尝试本地持仓", cycle.ID[:8], bErr)
					// 交易所查询失败时回退到本地
					holdings, hErr := s.repo.ListHoldings(ctx)
					if hErr == nil {
						for _, h := range holdings {
							if strings.EqualFold(h.Pair, pair) && h.Quantity > 0 {
								execInput.SellQuantity = h.Quantity
								log.Printf("[周期:%s] 📦 平仓(本地回退): %s 数量=%.4f", cycle.ID[:8], pair, h.Quantity)
								break
							}
						}
					}
				}
			}
		}

		if execInput.SellQuantity <= 0 {
			log.Printf("[周期:%s] ⚠ 平仓跳过: %s 无持仓可卖", cycle.ID[:8], pair)
			_ = addLog("执行", "平仓跳过: 无持仓可卖")
			_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusSuccess, "")
			return domain.CycleResult{
				Cycle:  cycle,
				Signal: sig,
				Risk:   riskDecision,
				Logs:   logs,
			}, nil
		}
	}

	log.Printf("[周期:%s] 🚀 执行: 正在下单 方向=%s 金额=%.2f 数量=%.4f ...", cycle.ID[:8], sig.Side, execInput.StakeUSDT, execInput.SellQuantity)
	ord, execErr := s.executor.Execute(ctx, execInput)
	if ord.ID != "" {
		_ = s.repo.InsertOrder(ctx, ord)
	}
	if execErr != nil {
		log.Printf("[周期:%s] ✘ 下单失败: %v", cycle.ID[:8], execErr)
		_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusFailed, execErr.Error())
		_ = addLog("执行", "下单失败: "+execErr.Error())
		return domain.CycleResult{}, execErr
	}

	log.Printf("[周期:%s] ✔ 执行: 订单状态=%s 交易所ID=%s", cycle.ID[:8], ord.Status, ord.ExchangeOrderID)
	_ = addLog("执行", fmt.Sprintf("订单状态=%s 交易所ID=%s", ord.Status, ord.ExchangeOrderID))
	_ = s.repo.UpdateCycleStatus(ctx, cycle.ID, domain.CycleStatusSuccess, "")
	cycle.Status = domain.CycleStatusSuccess
	cycle.UpdatedAt = time.Now().UTC()

	// 交易成功后更新持仓
	s.UpdateHoldingAfterTrade(ctx, ord)

	log.Printf("[周期:%s] ■ 执行完毕 状态=成功 总耗时=%s", cycle.ID[:8], time.Since(cycleStart))
	return domain.CycleResult{
		Cycle:  cycle,
		Signal: sig,
		Risk:   riskDecision,
		Order:  &ord,
		Logs:   logs,
	}, nil
}

func (s *Service) GetCycleReport(ctx context.Context, cycleID string) (domain.CycleReport, error) {
	return s.repo.GetCycleReport(ctx, cycleID)
}

func (s *Service) DeleteCycle(ctx context.Context, cycleID string) error {
	return s.repo.DeleteCycle(ctx, cycleID)
}

func (s *Service) ListPositions(ctx context.Context, limit int) ([]domain.PositionView, error) {
	return s.repo.ListPositions(ctx, limit)
}

// TradingInfo 返回当前交易模式信息
type TradingInfo struct {
	Mode     string `json:"mode"`     // "spot" 或 "futures"
	Leverage int    `json:"leverage"` // 杠杆倍数
	DryRun   bool   `json:"dry_run"`  // 是否模拟模式
}

func (s *Service) GetTradingInfo() TradingInfo {
	return TradingInfo{
		Mode:     s.executor.TradingMode(),
		Leverage: s.executor.Leverage(),
		DryRun:   s.executor.IsDryRun(),
	}
}

// ListCycles 分页获取历史周期列表
func (s *Service) ListCycles(ctx context.Context, page, pageSize int) ([]domain.CycleSummary, int, error) {
	total, err := s.repo.CountCycles(ctx)
	if err != nil {
		return nil, 0, err
	}
	cycles, err := s.repo.ListCycles(ctx, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	return cycles, total, nil
}

// ==================== 账户余额 ====================

// AccountBalance 账户余额视图
type AccountBalance struct {
	Symbol string  `json:"symbol"`
	Free   float64 `json:"free"`
	Locked float64 `json:"locked"`
	Total  float64 `json:"total"`
}

// GetAccountBalances 从交易所获取完整余额
func (s *Service) GetAccountBalances(ctx context.Context) ([]AccountBalance, error) {
	rawBalances, err := s.executor.FetchFullBalance(ctx)
	if err != nil {
		return nil, err
	}
	balances := make([]AccountBalance, 0, len(rawBalances))
	for _, b := range rawBalances {
		balances = append(balances, AccountBalance{
			Symbol: b.Symbol,
			Free:   b.Free,
			Locked: b.Locked,
			Total:  b.Total,
		})
	}
	return balances, nil
}

// ==================== 持仓管理 ====================

// ResetData 清空所有数据
func (s *Service) ResetData(ctx context.Context) error {
	if err := s.repo.ResetAllData(ctx); err != nil {
		return err
	}
	log.Println("[数据] ✔ 所有数据已清空")
	return nil
}

// SyncHoldings 同步持仓数据（模拟盘从订单聚合，实盘从交易所同步）
func (s *Service) SyncHoldings(ctx context.Context) error {
	if s.executor.IsDryRun() {
		return s.syncHoldingsFromOrders(ctx)
	}
	return s.syncHoldingsFromExchange(ctx)
}

// SyncHoldingsForceExchange 强制从交易所同步（忽略 dry-run 设置）
func (s *Service) SyncHoldingsForceExchange(ctx context.Context) error {
	return s.syncHoldingsFromExchange(ctx)
}

// SyncTradesFromExchange 从币安同步成交记录，并自动更新持仓
func (s *Service) SyncTradesFromExchange(ctx context.Context, pair string) (int, error) {
	trades, err := s.executor.FetchTradeHistory(ctx, pair, 500)
	if err != nil {
		return 0, fmt.Errorf("获取交易记录失败: %w", err)
	}

	imported := 0
	for _, t := range trades {
		// 用 "binance-{tradeID}" 作为 exchange_order_id 去重
		exID := fmt.Sprintf("binance-%d", t.TradeID)
		exists, _ := s.repo.OrderExistsByExchangeID(ctx, exID)
		if exists {
			continue
		}

		side := domain.SideLong
		if !t.IsBuyer {
			side = domain.SideClose
		}

		// 还原 pair 格式 "DOGEUSDT" → "DOGE/USDT"
		pairFmt := pair
		if !strings.Contains(pair, "/") {
			// 尝试从 symbol 推断
			pairFmt = strings.TrimSuffix(t.Symbol, "USDT") + "/USDT"
		}

		order := domain.Order{
			ID:              uuid.NewString(),
			CycleID:         "", // 外部交易，无周期
			SignalID:        "",
			ClientOrderID:   fmt.Sprintf("binance-ord-%d", t.OrderID),
			Pair:            pairFmt,
			Side:            side,
			StakeUSDT:       t.QuoteQty,
			Status:          "filled",
			ExchangeOrderID: exID,
			FilledPrice:     t.Price,
			FilledQuantity:  t.Quantity,
			RawResponse:     fmt.Sprintf(`{"trade_id":%d,"order_id":%d}`, t.TradeID, t.OrderID),
			CreatedAt:       t.Timestamp,
		}

		if err := s.repo.InsertOrder(ctx, order); err != nil {
			log.Printf("[同步] 插入交易记录失败 trade=%d: %v", t.TradeID, err)
			continue
		}
		imported++
	}

	log.Printf("[同步] %s 共 %d 笔成交，新导入 %d 笔", pair, len(trades), imported)

	// 同步完成后重新聚合持仓
	if imported > 0 {
		if err := s.syncHoldingsFromOrders(ctx); err != nil {
			log.Printf("[同步] 重新聚合持仓失败: %v", err)
		}
	}

	return imported, nil
}

// syncHoldingsFromOrders 从本地订单历史聚合持仓（模拟盘）
func (s *Service) syncHoldingsFromOrders(ctx context.Context) error {
	holdings, err := s.repo.AggregateHoldingsFromOrders(ctx)
	if err != nil {
		return fmt.Errorf("聚合订单持仓: %w", err)
	}
	for _, h := range holdings {
		if err := s.repo.UpsertHolding(ctx, h); err != nil {
			return fmt.Errorf("更新持仓 %s: %w", h.Pair, err)
		}
	}
	log.Printf("[持仓] 从订单历史同步完成，共 %d 个币对", len(holdings))
	return nil
}

// syncHoldingsFromExchange 从 Binance 交易所同步真实余额（实盘）
func (s *Service) syncHoldingsFromExchange(ctx context.Context) error {
	balances, err := s.executor.FetchAccountBalances(ctx)
	if err != nil {
		log.Printf("[持仓] ⚠ 交易所同步失败: %v，尝试从订单聚合", err)
		return s.syncHoldingsFromOrders(ctx)
	}

	now := time.Now().UTC()
	count := 0
	for _, b := range balances {
		pair := b.Symbol + "/USDT"
		h := domain.Holding{
			Pair:      pair,
			Symbol:    b.Symbol,
			Quantity:  b.Total,
			AvgPrice:  0, // 交易所不返回均价，后续从订单补充
			TotalCost: 0,
			Source:    "exchange",
			UpdatedAt: now,
		}
		if err := s.repo.UpsertHolding(ctx, h); err != nil {
			log.Printf("[持仓] 更新 %s 失败: %v", pair, err)
			continue
		}
		count++
	}
	log.Printf("[持仓] 从交易所同步完成，共 %d 个币对", count)
	return nil
}

// GetHoldings 获取持仓列表，附带实时行情
func (s *Service) GetHoldings(ctx context.Context) ([]domain.HoldingView, error) {
	holdings, err := s.repo.ListHoldings(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]domain.HoldingView, 0, len(holdings))
	for _, h := range holdings {
		view := domain.HoldingView{Holding: h}

		// 获取实时价格
		symbol := strings.Replace(h.Pair, "/", "", 1)
		price, pErr := s.fetchTickerPrice(ctx, symbol)
		if pErr == nil && price > 0 {
			view.CurrentPrice = price
			view.MarketValue = h.Quantity * price
			view.UnrealizedPnL = view.MarketValue - h.TotalCost
			if h.TotalCost > 0 {
				view.PnLPercent = (view.UnrealizedPnL / h.TotalCost) * 100
			}
		}
		views = append(views, view)
	}
	return views, nil
}

// UpdateHoldingAfterTrade 交易成功后更新持仓
func (s *Service) UpdateHoldingAfterTrade(ctx context.Context, order domain.Order) {
	if order.FilledPrice <= 0 || order.FilledQuantity <= 0 {
		return
	}

	// 从 DB 获取现有持仓
	holdings, _ := s.repo.ListHoldings(ctx)
	var existing *domain.Holding
	for i, h := range holdings {
		if h.Pair == order.Pair {
			existing = &holdings[i]
			break
		}
	}

	now := time.Now().UTC()
	symbol := strings.Split(order.Pair, "/")[0]

	if order.Side == domain.SideLong {
		// 买入：增加持仓
		if existing != nil {
			newQty := existing.Quantity + order.FilledQuantity
			newCost := existing.TotalCost + (order.FilledQuantity * order.FilledPrice)
			_ = s.repo.UpsertHolding(ctx, domain.Holding{
				Pair:      order.Pair,
				Symbol:    symbol,
				Quantity:  newQty,
				AvgPrice:  newCost / newQty,
				TotalCost: newCost,
				Source:    "local",
				UpdatedAt: now,
			})
		} else {
			_ = s.repo.UpsertHolding(ctx, domain.Holding{
				Pair:      order.Pair,
				Symbol:    symbol,
				Quantity:  order.FilledQuantity,
				AvgPrice:  order.FilledPrice,
				TotalCost: order.FilledQuantity * order.FilledPrice,
				Source:    "local",
				UpdatedAt: now,
			})
		}
		log.Printf("[持仓] 买入更新 %s: +%.4f @ %.8f", order.Pair, order.FilledQuantity, order.FilledPrice)
	} else if order.Side == domain.SideClose {
		// 卖出：减少持仓
		if existing != nil {
			newQty := existing.Quantity - order.FilledQuantity
			if newQty < 0 {
				newQty = 0
			}
			ratio := order.FilledQuantity / existing.Quantity
			if ratio > 1 {
				ratio = 1
			}
			newCost := existing.TotalCost * (1 - ratio)
			avgPrice := 0.0
			if newQty > 0 {
				avgPrice = newCost / newQty
			}
			_ = s.repo.UpsertHolding(ctx, domain.Holding{
				Pair:      order.Pair,
				Symbol:    symbol,
				Quantity:  newQty,
				AvgPrice:  avgPrice,
				TotalCost: newCost,
				Source:    "local",
				UpdatedAt: now,
			})
			log.Printf("[持仓] 卖出更新 %s: -%.4f 剩余=%.4f", order.Pair, order.FilledQuantity, newQty)
		}
	}
}

// fetchTickerPrice 从 Binance 获取当前价格
// fetchAccountDataForPrompt 获取真实余额和持仓数据，用于填充 AI 提示词
func (s *Service) fetchAccountDataForPrompt(ctx context.Context, pair string) (float64, []market.PositionData) {
	var usdtBalance float64

	// 1. 获取 USDT 余额
	balances, err := s.executor.FetchFullBalance(ctx)
	if err != nil {
		log.Printf("[账户] ⚠ 获取余额失败: %v，使用默认值 0", err)
	} else {
		for _, b := range balances {
			if b.Symbol == "USDT" {
				usdtBalance = b.Free
				break
			}
		}
	}

	// 2. 获取当前持仓
	var positions []market.PositionData

	// 合约实盘模式：优先从 positionRisk API 获取
	if s.executor.TradingMode() == "futures" && !s.executor.IsDryRun() {
		posAmt, pErr := s.executor.FetchPositionRisk(ctx, pair)
		if pErr == nil && posAmt > 0 {
			sym := strings.Replace(pair, "/", "", 1)
			currentPrice, _ := s.fetchTickerPrice(ctx, sym)
			leverage := s.executor.Leverage()
			positions = append(positions, market.PositionData{
				Symbol:        pair,
				Side:          "LONG",
				Quantity:      fmt.Sprintf("%.4f", posAmt),
				EntryPrice:    "N/A",
				CurrentPrice:  fmt.Sprintf("%.6f", currentPrice),
				UnrealizedPnl: fmt.Sprintf("x%d leverage", leverage),
				Leverage:      fmt.Sprintf("%d", leverage),
			})
		}
	} else {
		// 现货模式或 dry-run：从本地 holdings 表获取
		holdings, hErr := s.repo.ListHoldings(ctx)
		if hErr != nil {
			log.Printf("[账户] ⚠ 获取持仓失败: %v", hErr)
			return usdtBalance, nil
		}
		for _, h := range holdings {
			if h.Quantity <= 0 {
				continue
			}
			sym := strings.Replace(h.Pair, "/", "", 1)
			currentPrice, pErr := s.fetchTickerPrice(ctx, sym)
			if pErr != nil {
				currentPrice = h.AvgPrice
			}

			// 计算持仓市值，过滤灰尘持仓（市值低于 1 USDT 的不计入）
			marketValue := h.Quantity * currentPrice
			if marketValue < 1.0 {
				log.Printf("[账户] ⚠ 忽略灰尘持仓: %s 数量=%.6f 市值=%.4f USDT < 1 USDT", h.Pair, h.Quantity, marketValue)
				continue
			}

			unrealizedPnL := (currentPrice - h.AvgPrice) * h.Quantity
			pnlPct := 0.0
			if h.TotalCost > 0 {
				pnlPct = (unrealizedPnL / h.TotalCost) * 100
			}

			leverage := fmt.Sprintf("%d", s.executor.Leverage())
			positions = append(positions, market.PositionData{
				Symbol:        h.Pair,
				Side:          "LONG",
				Quantity:      fmt.Sprintf("%.4f", h.Quantity),
				EntryPrice:    fmt.Sprintf("%.6f", h.AvgPrice),
				CurrentPrice:  fmt.Sprintf("%.6f", currentPrice),
				UnrealizedPnl: fmt.Sprintf("%.4f USDT (%.2f%%)", unrealizedPnL, pnlPct),
				Leverage:      leverage,
			})
		}
	}

	return usdtBalance, positions
}

func (s *Service) fetchTickerPrice(ctx context.Context, symbol string) (float64, error) {
	apiURL := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Price string `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	p, _ := strconv.ParseFloat(result.Price, 64)
	return p, nil
}

// fetchQuickTicker 快速从 Binance 获取 24h 价格和涨跌幅（轻量级，不含 K 线）
func fetchQuickTicker(ctx context.Context, pair string) (price, change float64, err error) {
	symbol := strings.ReplaceAll(strings.ToUpper(pair), "/", "")
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", symbol)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var ticker struct {
		LastPrice          string `json:"lastPrice"`
		PriceChangePercent string `json:"priceChangePercent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
		return 0, 0, err
	}

	price, _ = strconv.ParseFloat(ticker.LastPrice, 64)
	change, _ = strconv.ParseFloat(ticker.PriceChangePercent, 64)
	return price, change, nil
}

func fallbackSnapshot(pair string, in *domain.MarketSnapshot) domain.MarketSnapshot {
	if in == nil {
		return domain.MarketSnapshot{
			Pair:        pair,
			LastPrice:   0,
			Change24h:   0,
			Volume24h:   0,
			FundingRate: 0,
			Timestamp:   time.Now().UTC(),
		}
	}

	copy := *in
	if strings.TrimSpace(copy.Pair) == "" {
		copy.Pair = pair
	}
	if copy.Timestamp.IsZero() {
		copy.Timestamp = time.Now().UTC()
	}
	return copy
}
