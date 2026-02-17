package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai_quant/internal/auth"
	"ai_quant/internal/domain"
	"ai_quant/internal/orchestrator"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *orchestrator.Service
	timeout time.Duration
}

type runCycleRequest struct {
	Pair      string                 `json:"pair"`
	Snapshot  *domain.MarketSnapshot `json:"snapshot"`
	Portfolio domain.PortfolioState  `json:"portfolio"`
}

func NewRouter(service *orchestrator.Service, authService *auth.Service, timeoutSec int) *gin.Engine {
	router := gin.Default()

	h := &Handler{
		service: service,
		timeout: time.Duration(timeoutSec) * time.Second,
	}

	authHandler := NewAuthHandler(authService)

	// LLM 认证管理
	llmAuthManager := auth.GetGlobalAuthManager()
	llmAuthHandler := NewLLMAuthHandler(llmAuthManager)

	// Serve frontend static files
	router.Static("/static", "./client")
	router.GET("/", func(c *gin.Context) {
		c.File("./client/index.html")
	})

	// OAuth routes
	authGroup := router.Group("/auth")
	{
		authGroup.GET("/start", authHandler.startOAuth)
		authGroup.GET("/callback", authHandler.callback)
		authGroup.POST("/callback/manual", authHandler.manualCallback)
		authGroup.GET("/profiles", authHandler.listProfiles)
		authGroup.GET("/profiles/:provider", authHandler.getProfile)
		authGroup.DELETE("/profiles/:provider", authHandler.deleteProfile)
		authGroup.POST("/profiles/:provider/refresh", authHandler.refreshToken)
		authGroup.GET("/profiles/:provider/token", authHandler.getToken)
	}

	// LLM 认证管理路由
	llmAuthGroup := router.Group("/llm-auth")
	{
		llmAuthGroup.GET("/status", llmAuthHandler.getAuthStatus)
		llmAuthGroup.POST("/mode", llmAuthHandler.setAuthMode)
		llmAuthGroup.POST("/provider", llmAuthHandler.setAuthProvider)
	}

	v1 := router.Group("/api/v1")
	{
		v1.GET("/health", h.health)
		v1.POST("/cycles/run", h.runCycle)
		v1.GET("/cycles", h.listCycles)
		v1.GET("/cycles/:id", h.getCycle)
		v1.DELETE("/cycles/:id", h.deleteCycle)
		v1.GET("/positions", h.listPositions)
		v1.GET("/holdings", h.listHoldings)
		v1.POST("/holdings/sync", h.syncHoldings)
		v1.POST("/trades/sync", h.syncTrades)
		v1.GET("/balance", h.getBalance)
		v1.POST("/data/reset", h.resetData)
	}

	return router
}

func (h *Handler) health(c *gin.Context) {
	info := h.service.GetTradingInfo()
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"time":    time.Now().UTC(),
		"trading": info,
	})
}

func (h *Handler) runCycle(c *gin.Context) {
	var req runCycleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Pair = strings.TrimSpace(req.Pair)
	if req.Pair == "" {
		req.Pair = "BTC/USDT"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	result, err := h.service.RunCycle(ctx, orchestrator.RunRequest{
		Pair:      req.Pair,
		Snapshot:  req.Snapshot,
		Portfolio: req.Portfolio,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// listCycles 分页查询历史周期
func (h *Handler) listCycles(c *gin.Context) {
	page := 1
	pageSize := 15
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := c.Query("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	cycles, total, err := h.service.ListCycles(ctx, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalPages := (total + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"cycles":      cycles,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

func (h *Handler) getCycle(c *gin.Context) {
	cycleID := strings.TrimSpace(c.Param("id"))
	if cycleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing cycle id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	report, err := h.service.GetCycleReport(ctx, cycleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, report)
}

func (h *Handler) deleteCycle(c *gin.Context) {
	cycleID := strings.TrimSpace(c.Param("id"))
	if cycleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing cycle id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	if err := h.service.DeleteCycle(ctx, cycleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "cycle deleted successfully"})
}

func (h *Handler) listPositions(c *gin.Context) {
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	positions, err := h.service.ListPositions(ctx, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total":     len(positions),
		"positions": positions,
	})
}

// listHoldings 获取当前持仓汇总（含实时行情）
func (h *Handler) listHoldings(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	views, err := h.service.GetHoldings(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 计算汇总
	totalCost := 0.0
	totalValue := 0.0
	totalPnL := 0.0
	for _, v := range views {
		totalCost += v.TotalCost
		totalValue += v.MarketValue
		totalPnL += v.UnrealizedPnL
	}
	pnlPercent := 0.0
	if totalCost > 0 {
		pnlPercent = (totalPnL / totalCost) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"holdings":    views,
		"total_cost":  totalCost,
		"total_value": totalValue,
		"total_pnl":   totalPnL,
		"pnl_percent": pnlPercent,
	})
}

// syncHoldings 手动触发持仓同步
// 支持 ?source=exchange 强制从交易所同步（即使模拟模式）
// 支持 ?source=orders 强制从订单聚合
func (h *Handler) syncHoldings(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	source := c.Query("source")
	var err error
	switch source {
	case "exchange":
		err = h.service.SyncHoldingsForceExchange(ctx)
	default:
		err = h.service.SyncHoldings(ctx)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	msg := "持仓同步完成"
	if source == "exchange" {
		msg = "已从交易所同步持仓"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}

// syncTrades 从币安同步成交记录
func (h *Handler) syncTrades(c *gin.Context) {
	pair := c.DefaultQuery("pair", "DOGE/USDT")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	imported, err := h.service.SyncTradesFromExchange(ctx, pair)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "交易记录同步完成",
		"pair":     pair,
		"imported": imported,
	})
}

// getBalance 从交易所获取账户余额
func (h *Handler) getBalance(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	balances, err := h.service.GetAccountBalances(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 提取 USDT 余额
	usdtFree := 0.0
	usdtLocked := 0.0
	usdtTotal := 0.0
	assets := make([]gin.H, 0)
	for _, b := range balances {
		if b.Symbol == "USDT" {
			usdtFree = b.Free
			usdtLocked = b.Locked
			usdtTotal = b.Total
		}
		assets = append(assets, gin.H{
			"symbol": b.Symbol,
			"free":   b.Free,
			"locked": b.Locked,
			"total":  b.Total,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"usdt_free":   usdtFree,
		"usdt_locked": usdtLocked,
		"usdt_total":  usdtTotal,
		"assets":      assets,
	})
}

// resetData 清空所有数据
func (h *Handler) resetData(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	if err := h.service.ResetData(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "所有数据已清空"})
}
