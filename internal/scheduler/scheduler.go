package scheduler

import (
	"context"
	"log"
	"strings"
	"time"

	"ai_quant/internal/domain"
	"ai_quant/internal/orchestrator"
)

// Scheduler 定时自动执行交易周期
type Scheduler struct {
	service  *orchestrator.Service
	interval time.Duration
	pairs    []string
	stop     chan struct{}
}

// New 创建定时调度器
func New(service *orchestrator.Service, intervalSec int, pairsStr string) *Scheduler {
	pairs := []string{}
	for _, p := range strings.Split(pairsStr, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			pairs = append(pairs, strings.ToUpper(p))
		}
	}
	if len(pairs) == 0 {
		pairs = []string{"BTC/USDT"}
	}

	return &Scheduler{
		service:  service,
		interval: time.Duration(intervalSec) * time.Second,
		pairs:    pairs,
		stop:     make(chan struct{}),
	}
}

// Start 启动定时任务（非阻塞，在后台 goroutine 运行）
func (s *Scheduler) Start() {
	log.Printf("[定时器] 已启动 间隔=%s 交易对=%v", s.interval, s.pairs)

	go func() {
		// 启动后立即执行一次
		// s.runAll()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.runAll()
			case <-s.stop:
				log.Println("[定时器] 已停止")
				return
			}
		}
	}()
}

// Stop 停止定时任务
func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) runAll() {
	for _, pair := range s.pairs {
		s.runOnce(pair)
	}
}

func (s *Scheduler) runOnce(pair string) {
	log.Printf("[定时器] 自动执行 %s", pair)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := s.service.RunCycle(ctx, orchestrator.RunRequest{
		Pair:      pair,
		Snapshot:  nil,
		Portfolio: domain.PortfolioState{},
	})
	if err != nil {
		log.Printf("[定时器] ✘ %s 执行失败: %v", pair, err)
		return
	}

	log.Printf("[定时器] ✔ %s 执行完成 状态=%s 信号=%s 置信度=%.2f",
		pair, result.Cycle.Status, result.Signal.Side, result.Signal.Confidence)
}
