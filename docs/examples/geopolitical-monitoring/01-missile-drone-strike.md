# Flow: Missile / Drone Strike

**Domain prefix:** `strike_`
**Trigger:** `new_tweet`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Strike Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a military intelligence analyst monitoring the Iran-US-Israel conflict.
Your task: determine whether the tweet reports a CONFIRMED or CREDIBLY CLAIMED missile or drone strike that has actually been launched or has already hit a target.

Return ONLY valid JSON, no explanation, no markdown:
{"strike_is_strike": "true|false", "strike_classifier_confidence": "0.0-1.0"}

Rules:
- "strike_is_strike": "true" only if the tweet reports a confirmed/claimed missile launch, drone swarm launch, or drone/missile impact
- "strike_is_strike": "false" for threats, warnings, air defense activations without incoming, drills, historical references, speculation
- "strike_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "Iran warns it will respond if attacked" → {"strike_is_strike": "false", "strike_classifier_confidence": "0.96"}
- "BREAKING: Iran launches ballistic missiles toward Israel, sirens in Tel Aviv" → {"strike_is_strike": "true", "strike_classifier_confidence": "0.98"}
- "IDF confirms 120 drones intercepted over northern Israel" → {"strike_is_strike": "true", "strike_classifier_confidence": "0.91"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `strike_is_strike` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Strike Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a military intelligence analyst. A tweet has been confirmed to report a missile or drone strike. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "strike_weapon_type": "ballistic_missile|cruise_missile|shahed_drone|loitering_munition|unknown",
  "strike_quantity": "<numeric string or unknown>",
  "strike_origin_country": "Iran|Israel|US|Houthi|Hezbollah|Hamas|Iraq|unknown",
  "strike_origin_location": "<city, base, region, or unknown>",
  "strike_target_country": "Iran|Israel|US|Saudi_Arabia|Iraq|Syria|Yemen|unknown",
  "strike_target_location": "<city, base, or region, or unknown>",
  "strike_target_type": "military_base|air_defense|nuclear_facility|port|city|unknown",
  "strike_intercepted": "true|false|partial|unknown",
  "strike_casualties": "<numeric string or null>",
  "strike_damage_reported": "true|false",
  "strike_escalation_indicators": ["<short phrase>"]
}

Rules:
- Use "unknown" for any scalar field not clearly stated
- "strike_casualties": the number mentioned; use "null" if not mentioned
- "strike_intercepted": "partial" if some were intercepted, not all
- "strike_escalation_indicators": short phrases indicating widening conflict; empty array [] if none
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
🚀 <b>Missile / Drone Strike Detected</b>

<b>Weapon:</b> ${strike_weapon_type} × ${strike_quantity}
<b>Origin:</b> ${strike_origin_country} — ${strike_origin_location}
<b>Target:</b> ${strike_target_country} — ${strike_target_location} (${strike_target_type})
<b>Intercepted:</b> ${strike_intercepted}
<b>Casualties:</b> ${strike_casualties}
<b>Damage reported:</b> ${strike_damage_reported | yesno}
<b>Escalation signals:</b> ${strike_escalation_indicators | join}

<blockquote expandable>${tweet_short}</blockquote>

🐦️ <a href="${tweet_url}">Check on X</a>

#strike #${strike_weapon_type} #${strike_origin_country}
```
