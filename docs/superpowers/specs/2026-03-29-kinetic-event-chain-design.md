# Kinetic Event Detection & Extraction Chain

**Date:** 2026-03-29
**Status:** Approved

## Overview

A tweet processing chain that detects kinetic military events (missile launches, airstrikes, drone attacks, explosions) and extracts structured intelligence from matching tweets, then fires a notification alert.

Triggered on every incoming tweet. Uses two sequential Ollama pipelines — a fast detection pass followed by a richer extraction pass — gated by a condition node so extraction only runs on confirmed attack reports.

## Chain Graph

```
[Start]
  ↓
[Action: Detect Kinetic Event]          ← Ollama pipeline
  ↓
[Condition: is_attack == "true"]
  ├─ (pass)
  │    ↓
  │  [Action: Extract Attack Details]   ← Ollama pipeline
  │    ↓
  │  [Action: Notify Alert]             ← notification pipeline
  └─ (fail) → end
```

**Trigger link:** `new_tweet` → this chain

## Pipelines

### 1. Detect Kinetic Event (Ollama)

**Purpose:** Binary classifier — does this tweet report a real kinetic military event?

**Prompt guidance:**
- Ask whether the tweet reports an actual kinetic military event: missile launch, airstrike, drone attack, or explosion at a military/strategic target
- Distinguish confirmed reports from threats, rumors, or speculative language
- Output strict JSON with no extra text

**Expected output (stored in `action_execution_metadata`):**
```json
{
  "is_attack": "true",
  "confidence": "0.92"
}
```

All values are strings (Ollama JSON output is stored as string map in context).

### 2. Extract Attack Details (Ollama)

**Purpose:** Structured intelligence extraction from confirmed attack tweets.

**Prompt guidance:**
- Extract all available details; use `"unknown"` for fields not mentioned in the tweet
- `escalation_indicators` should be an array of short phrases (e.g., `["second strike in 24h", "capital targeted"]`); empty array if none
- `casualties` should be a numeric string or `"null"` if not mentioned
- Output strict JSON with no extra text

**Expected output:**
```json
{
  "attack_type": "missile|drone|airstrike|explosion|unknown",
  "weapon_system": "ballistic|cruise|kamikaze_drone|unknown",
  "attacker": "Iran|Israel|US|Houthi|Hezbollah|Hamas|unknown",
  "target_location": "<city or region name>",
  "target_type": "military|civilian|infrastructure|unknown",
  "casualties": "<number or null>",
  "response_mentioned": "true|false",
  "escalation_indicators": ["..."]
}
```

### 3. Notify Alert (notification pipeline)

**Purpose:** Push an immediate alert with the extracted details.

**Message template** (Telegram or Pushover):
```
⚔️ Kinetic Event Detected
Type: ${attack_type} (${weapon_system})
Attacker: ${attacker}
Target: ${target_location} (${target_type})
Casualties: ${casualties}
Response mentioned: ${response_mentioned}
Escalation: ${escalation_indicators}
```

Uses `${key}` substitution from the merged chain context, which contains all outputs from both upstream Ollama actions.

## Condition Node

| Key | Operator | Value |
|-----|----------|-------|
| `is_attack` | `==` | `"true"` |

Single condition; AND logic (only one condition needed here).

## Data Flow

1. `new_tweet` trigger fires for every incoming tweet
2. Worker runs **Detect Kinetic Event** → outputs `{is_attack, confidence}` into context
3. Condition node checks `is_attack == "true"`
   - **Fail:** chain ends, no further work
   - **Pass:** worker runs **Extract Attack Details** → outputs full intelligence struct into context
4. Worker runs **Notify Alert** → sends push notification with substituted template vars
5. All action outputs persist in `action_execution_metadata` for later querying

## Setup Steps

1. Create Ollama pipeline: **Detect Kinetic Event** (with detection prompt)
2. Create Ollama pipeline: **Extract Attack Details** (with extraction prompt)
3. Create notification channel for alerts (Telegram or Pushover)
4. Create notification pipeline: **Notify Alert** (using message template above)
5. Create actions wrapping each pipeline (one action per pipeline)
6. Create chain: **Kinetic Event Chain**
7. Build graph: start → detect action → condition node → extract action → notify action
8. Create trigger link: `new_tweet` → Kinetic Event Chain, active

## Notes

- The detection stage acts as a cheap filter — most tweets will fail the condition and incur only one Ollama call
- Both Ollama outputs accumulate in the chain context, so the notification action can reference fields from either extraction step
- `escalation_indicators` is a JSON array serialized as a string in the context; the notification template will render it as-is (e.g., `["second strike in 24h"]`) — acceptable for initial version
- If false-positive rate is high, a confidence threshold condition (`confidence >= "0.7"`) can be added as a second condition on the condition node without changing the rest of the chain
