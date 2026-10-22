# Example: Geopolitical Conflict Monitoring

A worked example of tidewatch pipelines, built for real-time monitoring of the 2025–2026 Iran / US / Israel conflict. It shows the Detector → Condition → Extractor → Notification pattern described in [chain-pipeline-standards.md](../../chain-pipeline-standards.md); the same structure applies to any domain (disaster response, markets, product launches, etc.).

Each file defines a complete Detector + Extractor chain: detector system prompt, condition node, extractor system prompt, and Telegram notification template.

All flows use `model: qwen2.5:7b-instruct`, `usecase: custom`, trigger `new_signal`.

| # | File | Domain prefix | Detector condition |
|---|---|---|---|
| 1 | [01-missile-drone-strike.md](01-missile-drone-strike.md) | `strike_` | `strike_is_strike == "true"` |
| 2 | [02-nuclear-activity.md](02-nuclear-activity.md) | `nuclear_` | `nuclear_is_development == "true"` |
| 3 | [03-proxy-force-activity.md](03-proxy-force-activity.md) | `proxy_` | `proxy_is_operation == "true"` |
| 4 | [04-escalation-rhetoric.md](04-escalation-rhetoric.md) | `rhetoric_` | `rhetoric_is_escalation == "true"` |
| 5 | [05-cyber-infrastructure-attack.md](05-cyber-infrastructure-attack.md) | `cyber_` | `cyber_is_attack == "true"` |
| 6 | [06-diplomatic-ceasefire.md](06-diplomatic-ceasefire.md) | `diplomatic_` | `diplomatic_is_development == "true"` |
| 7 | [07-sanctions-economic-pressure.md](07-sanctions-economic-pressure.md) | `sanctions_` | `sanctions_is_action == "true"` |
| 8 | [08-military-mobilization.md](08-military-mobilization.md) | `mobilization_` | `mobilization_is_deployment == "true"` |
| 9 | [09-hostage-prisoner-event.md](09-hostage-prisoner-event.md) | `hostage_` | `hostage_is_event == "true"` |
| 10 | [10-casualty-report.md](10-casualty-report.md) | `casualty_` | `casualty_is_report == "true"` |
