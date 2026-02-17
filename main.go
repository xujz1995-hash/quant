package main

import (
	"context"
	"log"

	"ai_quant/internal/agent/execution"
	"ai_quant/internal/agent/position"
	"ai_quant/internal/agent/risk"
	"ai_quant/internal/agent/signal"
	"ai_quant/internal/auth"
	"ai_quant/internal/config"
	httpapi "ai_quant/internal/http"
	"ai_quant/internal/orchestrator"
	"ai_quant/internal/scheduler"
	"ai_quant/internal/store"
)

func main() {
	cfg := config.Load()

	repo, err := store.NewSQLiteRepository(cfg.SQLiteDSN)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer repo.Close()

	if err := repo.Init(context.Background()); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	// 初始化 OAuth 服务（需要在 signal agent 之前）
	authService, err := auth.NewService(cfg.OAuthStoragePath)
	if err != nil {
		log.Fatalf("初始化 OAuth 服务失败: %v", err)
	}
	log.Println("🔐 OAuth 服务已启动")

	// 初始化全局 LLM 认证管理器
	authMode := auth.AuthMode(cfg.LLMAuthMode)
	provider := auth.Provider(cfg.LLMAuthProvider)
	auth.InitGlobalAuthManager(authService, cfg.OpenAIAPIKey, authMode, provider)
	log.Printf("🔑 LLM 认证管理器已初始化 模式=%s 提供商=%s", authMode, provider)

	signalAgent := signal.NewWithAuth(cfg, authService)
	riskAgent := risk.New(cfg)
	positionAgent := position.New()

	// 根据交易模式选择 Executor
	var execAgent execution.Executor
	if cfg.TradingMode == "futures" {
		execAgent = execution.NewFutures(cfg)
		log.Printf("📈 交易模式: USDT-M 永续合约 (%dx 杠杆)", cfg.FuturesLeverage)
	} else {
		execAgent = execution.New(cfg)
		log.Println("📈 交易模式: 现货交易")
	}

	service := orchestrator.New(repo, signalAgent, riskAgent, positionAgent, execAgent)

	// 启动时同步持仓（holdings 表为空则自动同步）
	holdings, _ := repo.ListHoldings(context.Background())
	if len(holdings) == 0 {
		log.Println("[持仓] holdings 表为空，正在同步 ...")
		if err := service.SyncHoldings(context.Background()); err != nil {
			log.Printf("[持仓] ⚠ 初始同步失败: %v", err)
		}
	} else {
		log.Printf("[持仓] 已有 %d 条持仓记录", len(holdings))
	}

	// 启动定时自动交易
	if cfg.AutoRunEnabled {
		sched := scheduler.New(service, cfg.AutoRunInterval, cfg.AutoRunPairs)
		sched.Start()
		defer sched.Stop()
	} else {
		log.Println("[定时器] 未启用，设置 AUTO_RUN_ENABLED=true 开启自动交易")
	}

	router := httpapi.NewRouter(service, authService, cfg.RequestTimeoutSec)

	log.Printf("AI Quant 服务启动 地址=%s 模式=%s 模拟=%v", cfg.HTTPAddr, cfg.TradingMode, cfg.DryRun)
	if err := router.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
