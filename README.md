# ai_quant MVP (Go + Gin + LangChainGo + SQLite + Freqtrade)

This repository provides a minimal multi-agent crypto trading control plane:

- `SignalAgent` (LangChainGo when `OPENAI_API_KEY` is set, otherwise rule-based fallback)
- `RiskAgent` (hard rule gate)
- `ExecutionAgent` (Freqtrade adapter; defaults to dry-run)
- `Orchestrator` (pipeline: signal -> risk -> execute)

## Quick start

```bash
go mod tidy
go run .
```

Server starts on `:8080` by default.

## API

### Health

```bash
curl http://localhost:8080/api/v1/health
```

### Run one cycle

```bash
curl -X POST http://localhost:8080/api/v1/cycles/run \
  -H 'Content-Type: application/json' \
  -d '{
    "pair": "BTC/USDT",
    "snapshot": {
      "last_price": 64500,
      "change_24h": 2.3,
      "volume_24h": 95000000,
      "funding_rate": 0.004
    },
    "portfolio": {
      "daily_pnl_usdt": -10,
      "open_exposure_usdt": 20
    }
  }'
```

### Query cycle report

```bash
curl http://localhost:8080/api/v1/cycles/<cycle_id>
```

## Environment variables

- `HTTP_ADDR` (default `:8080`)
- `SQLITE_DSN` (default `file:./ai_quant.db?_pragma=busy_timeout(5000)`)
- `REQUEST_TIMEOUT_SEC` (default `15`)
- `OPENAI_API_KEY` (optional; when set, SignalAgent uses LangChainGo + OpenAI)
- `OPENAI_MODEL` (default `gpt-4o-mini`)
- `FREQTRADE_BASE_URL` (default `http://127.0.0.1:8081`)
- `FREQTRADE_ORDER_PATH` (default `/api/v1/forceenter`)
- `FREQTRADE_TOKEN` (optional bearer token)
- `FREQTRADE_USERNAME` / `FREQTRADE_PASSWORD` (optional basic auth)
- `DEFAULT_STAKE_USDT` (default `50`)
- `MAX_DAILY_LOSS_USDT` (default `100`)
- `MAX_EXPOSURE_USDT` (default `200`)
- `MIN_CONFIDENCE` (default `0.55`)
- `DRY_RUN` (default `true`)

## Notes

- This is an MVP control plane and uses SQLite for single-node usage.
- Keep `DRY_RUN=true` for initial validation.
- In production, add stronger portfolio/risk checks and migrate DB to PostgreSQL.
