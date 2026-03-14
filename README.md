# Bankai: SynthData-Native Up/Down Intelligence Engine

Bankai is a high-frequency **Up/Down market intelligence and execution system** built for short-horizon prediction markets.  
This system is intentionally designed as a **SynthData-first architecture**: SynthData is not a decorative add-on, it is a core probabilistic substrate that drives both our deterministic engine and our LLM execution layer.

---

## What This Project Is

Bankai fuses four layers into one production pipeline:

1. **Real-time market microstructure** from Polymarket (quotes, depth, spread, slippage)
2. **Synthetic probabilistic intelligence** from SynthData
3. **Deterministic mathematical decision engine** (EV, confidence, dynamic thresholding, Kelly sizing)
4. **LLM directional engine** constrained by deterministic baselines, hard risk guards, and closed-loop calibration

The result is a system that can produce and stream:
- window-aware UP/DOWN probabilities
- risk-aware trade recommendations
- execution-ready limit price + size
- explainable reason codes and invalidation conditions

---

## Why This Is A Strong SynthData-Native System

### SynthData is the canonical source for directional signal quality
For 5m/15m/1h Up/Down windows, Bankai uses SynthData as the primary source of predictive structure:

- Direct window signal:  
  `/insights/polymarket/up-down/{5min|15min|hourly}`
- Distribution priors:  
  `/insights/prediction-percentiles`
- Volatility priors:  
  `/insights/volatility`
- LP probability anchors:  
  `/insights/lp-probabilities`
- Enterprise path estimation:  
  `/v2/prediction/best`
- Historical calibration loop:  
  `/v2/prediction/historical`

### We enforce strict synth integrity (production-relevant detail)
This is a deliberate quality choice:

- Window mismatch payloads are rejected.
- Stale synth clocks are flagged.
- For direct Up/Down windows, if SynthData direct signal is missing, we **do not silently backfill with weaker proxies**.
- Instead, risk flags escalate and recommendation can hard-fail to `NO_TRADE`.

This fail-fast behavior protects traders from false confidence and highlights SynthData as execution-critical infrastructure.

---

## Architecture

```text
Polymarket RTDS + Gamma + Orderbook
           |
           v
   UpDownService (deterministic core)
   - market classification (BTC/ETH/SOL/XRP, 5m/15m/1h/4h)
   - synth ingestion + cache + budgeting
   - probability blending + EV + Kelly + guards
           |
     +-----+------------------+
     |                        |
     v                        v
Deterministic Recs      UpDownLLMService
(/updown/signal,        - multi-snapshot context
 /updown/recommendations) - Allora prior proxy
                          - deterministic baseline parity
                          - strict JSON decision packet
                          (/updown/llm/*)
```

---

## Deterministic Engine: Core Math

### 1) Market-implied directional probability

\[
p_{market,up} =
\begin{cases}
\frac{ask_{up}}{ask_{up}+ask_{down}} & ask_{up}, ask_{down} > 0 \\
ask_{up} & ask_{up}>0, ask_{down}\le0 \\
1-ask_{down} & ask_{down}>0, ask_{up}\le0
\end{cases}
\]

Clamped to \([0.01, 0.99]\).

### 2) Synth-enhanced blended probability

\[
p_{final,up} = \frac{\sum_i w_i p_i}{\sum_i w_i}
\]

Where \(p_i \in \{p_{market}, p_{synth}, p_{model}, p_{lp}\}\) with default weights:

- \(w_{market}=0.18\) (drops to 0.08 if executable quotes are missing)
- \(w_{synth}=0.34\) (drops to 0.18 if synth is stale)
- \(w_{model}=0.30\)
- \(w_{lp}=0.18\)

### 3) Consensus confidence from cross-model agreement

\[
\sigma = \sqrt{\frac{\sum_i w_i (p_i-\mu)^2}{\sum_i w_i}}, \quad
consensus = 1 - clamp(3\sigma, 0, 0.9)
\]
\[
confidence_{raw} = 0.25 + 0.75 \cdot consensus
\]

Then penalized by spread/depth/integrity flags and adjusted by volatility + calibration.

### 4) Binary expected value (after fees)

\[
EV = p_{win}\cdot(1-ask-fee) - (1-p_{win})\cdot ask
\]

Computed for both UP and DOWN sides.

### 5) Dynamic EV thresholding (regime + time + volatility + calibration)

Base threshold starts at `UPDOWN_EV_MIN_THRESHOLD` (default 0.0125), then increased by:

- regime (`volatile`, `momentum`, `transitional`, `mean_reversion`)
- near-expiry penalties
- volatility forecast bands
- calibration edge buffer

The final threshold is clamped (max 0.14), creating a context-aware minimum edge to trade.

### 6) Kelly-based sizing with hard caps

Raw Kelly:
\[
kelly_{raw} = \frac{p_{win}-ask}{1-ask}
\]

Capped Kelly:
\[
kelly = kelly_{raw}\cdot kelly\_fraction \cdot (0.45 + 0.55\cdot confidence)
\]
then constrained by:

- max fraction per trade
- per-asset exposure cap
- daily drawdown throttle

Notional:
\[
notional = bankroll \cdot kelly
\]

### 7) Risk-adjusted Sharpe on binary payoff

Win return:
\[
r_{win}=1-ask-fee
\]
Loss return:
\[
r_{loss}=-ask
\]
\[
Sharpe = \frac{\mathbb{E}[r]}{\sqrt{Var(r)}}
\]

Used in both deterministic diagnostics and LLM entry gating.

---

## How Deterministic Math Powers the LLM Engine

The LLM is not free-form. It is a constrained decision layer on top of deterministic science.

### A) Deterministic baseline is injected into LLM context
Each generation includes:

- deterministic formula version and blend model
- \(p_{final}\), EV, edge, Kelly, Sharpe
- baseline deterministic decision + reason codes

This enables **parity checks** and drift detection.

### B) Multi-snapshot stability before generation
For short windows, we collect repeated snapshots and compute drift:

- consensus drift
- ask-up drift
- ask-down drift
- best-EV drift

Hard instability blocks can force `NO_TRADE`.

### C) Allora prior mapped into 15m proxy
Allora 5m inference is transformed for 15m windows with decay and progress scaling:

\[
p15_{proxy}=clamp\left(0.5 + 0.5\cdot\left((2p5-1)\cdot 0.58 \cdot (0.65+0.35\cdot progress)\cdot e^{-\max(0,age-30)/180}\right), 0.01, 0.99\right)
\]

Freshness status (`fresh`, `stale_soft`, `stale_hard`) is a hard execution control.

### D) Entry gate score (execution realism)
Final `ready_to_bet` state is computed from weighted components:

- confidence
- retrieval quality
- freshness
- liquidity quality
- edge quality
- deviation quality
- disagreement penalty
- Sharpe quality

This converts LLM output into an institutional-style execution gate.

### E) Closed-loop calibration (LLM vs deterministic)
Daily shadow analytics compare deterministic and LLM outcomes with:

- EV delta
- Brier delta
- confidence scaling
- edge buffer adjustment

So the LLM is continuously measured against an auditable baseline.

---

## What Traders Get

- **Directionally aware UP/DOWN packets** for short windows
- **No-trade discipline** when evidence quality is insufficient
- **Execution-aware sizing** instead of naive fixed bet sizing
- **Explainability** via reason codes, risk flags, and invalidation conditions
- **Deterministic fallback and policy controls** (`det_allowed`, `llm_preferred`, `llm_only`)

For Up/Down traders, this means fewer low-quality entries and more consistent risk-adjusted decisions in fast markets.

---

## API Surfaces (Up/Down Core)

### Deterministic
- `GET /api/v1/updown/markets`
- `GET /api/v1/updown/market/:slug`
- `GET /api/v1/updown/signal/:slug`
- `GET /api/v1/updown/recommendations`
- `GET /api/v1/updown/stream` (SSE)
- `GET /api/v1/updown/performance`
- `POST /api/v1/updown/decisions`

### LLM
- `POST /api/v1/updown/llm/generate`
- `GET /api/v1/updown/llm/packet/:slug`
- `GET /api/v1/updown/llm/health`

Current LLM scope in production code:
- Assets: `BTC`, `ETH`
- Windows: `5m`, `15m`

---

## Local Setup

### 1) Prerequisites

- Go `1.24+`
- Bun `1.0+`
- PostgreSQL `15+` (with `uuid-ossp`; `pgvector` recommended)
- Redis `6+`

### 2) Backend env

```bash
cd backend
cp .env.example .env
```

Minimum required to run:

- `DATABASE_URL`
- `REDIS_URL`
- `AUTH_JWT_SECRET`
- `FRONTEND_URL`

Required for full Up/Down + LLM feature set:

- `SYNTHDATA_API_KEY`
- `OPENROUTER_API_KEY` (or `OPENAI_API_KEY`, depending `OPENAI_BASE_URL`)
- `UPDOWN_ENABLED=true`
- `UPDOWN_LLM_ENABLED=true`

Useful controls:

- `UPDOWN_EXECUTION_SOURCE_POLICY=det_allowed|llm_preferred|llm_only`
- `UPDOWN_SYNTH_MONTHLY_CREDIT_CAP=0` (0 = unlimited)

### 3) DB bootstrap

```bash
cd backend
psql "$DATABASE_URL" -f internal/db/schema.sql
for f in migrations/*.sql; do
  psql "$DATABASE_URL" -f "$f"
done
```

### 4) Run backend API

```bash
cd backend
go run ./cmd/api
```

### 5) Run worker (recommended for live streaming quality)

```bash
cd backend
go run ./cmd/worker
```

### 6) Run frontend

```bash
cd frontend
cp .env.example .env
bun install
bun run dev
```

Frontend runs on `http://localhost:3000`  
Backend runs on `http://localhost:8080`

---

## Quick Verification

### 1) Health
```bash
curl http://localhost:8080/api/v1/health
```

### 2) Discover an Up/Down market
```bash
curl "http://localhost:8080/api/v1/updown/markets?asset=BTC&window=5m"
```

### 3) Inspect deterministic signal (includes SynthData-derived fields)
```bash
curl "http://localhost:8080/api/v1/updown/signal/<slug>"
```

Look for:
- `p_synth_up`, `p_model_up`, `p_lp_up`, `p_final_up`
- `ev_up`, `ev_down`, `confidence`
- `risk_flags`, `reason_codes`

### 4) Generate LLM packet
```bash
curl -X POST "http://localhost:8080/api/v1/updown/llm/generate" \
  -H "Content-Type: application/json" \
  -d '{"slug":"<slug>","force_refresh":true}'
```

Look for:
- `decision`, `recommended_side`, `confidence`, `expected_value`
- `allora_proxy`, `snapshot_stability`
- `effective_guard_blocks`
- `entry.ready_to_bet`, `entry.entry_score`, `entry.gate_reasons`

### 5) Confirm LLM service health
```bash
curl "http://localhost:8080/api/v1/updown/llm/health"
```

---

## Repository Map

### Core deterministic engine
- `backend/internal/services/updown_service.go`

### LLM engine
- `backend/internal/services/updown_llm_service.go`

### SynthData integration
- `backend/internal/integrations/synthdata/client.go`

### Allora integration
- `backend/internal/integrations/allora/client.go`

### API routes + handlers
- `backend/internal/api/routes.go`
- `backend/internal/api/handlers/updown.go`

### Frontend Up/Down terminal
- `frontend/app/(terminal)/updown/page.tsx`
- `frontend/lib/updown-api.ts`

---

## Final Notes

This project’s core thesis is simple:

> **Short-horizon Up/Down trading should be volatility-first, probability-native, and guard-heavy.**

SynthData gives us the right primitives to do this correctly:

- direct directional probabilities
- distribution-aware uncertainty
- volatility context
- LP-informed structural priors
- historical calibration hooks

Bankai operationalizes those primitives into a rigorous deterministic engine and then scales them through a constrained LLM layer without sacrificing risk discipline.

That is why this is a genuine **SynthData-native system** for serious Up/Down traders.
