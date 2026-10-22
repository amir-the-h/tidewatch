# Flow: Proxy Force Activity

**Domain prefix:** `proxy_`
**Trigger:** `new_signal`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Proxy Force Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are an intelligence analyst monitoring Iran-backed proxy forces.
Your task: determine whether the text reports an ACTUAL OPERATION by an Iran-aligned proxy group — not general news about the group's existence or political statements.

Return ONLY valid JSON, no explanation, no markdown:
{"proxy_is_operation": "true|false", "proxy_classifier_confidence": "0.0-1.0"}

Rules:
- "proxy_is_operation": "true" if the text reports a confirmed or claimed attack, strike, ambush, rocket fire, maritime interdiction, or other active operation by Hezbollah, Houthis, Hamas, Kata'ib Hezbollah, IRGC-QF, Iraqi militias, or similar Iran-aligned group
- "proxy_is_operation": "false" for political statements, funding news, recruitment, historical background, sanctions designations, or threat rhetoric
- "proxy_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "Houthis fire anti-ship missile at US destroyer in Red Sea" → {"proxy_is_operation": "true", "proxy_classifier_confidence": "0.97"}
- "US sanctions Kata'ib Hezbollah leadership" → {"proxy_is_operation": "false", "proxy_classifier_confidence": "0.94"}
- "Hezbollah claims rocket barrage on northern Israel" → {"proxy_is_operation": "true", "proxy_classifier_confidence": "0.96"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `proxy_is_operation` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Proxy Force Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are an intelligence analyst. A post or article has been confirmed to report a proxy force operation. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "proxy_group": "Hezbollah|Houthi|Hamas|Kata_ib_Hezbollah|IRGC_QF|PIJ|Iraqi_militia|unknown",
  "proxy_operation_type": "rocket_fire|missile_launch|drone_attack|ambush|maritime_attack|airstrike|IED|unknown",
  "proxy_target_country": "Israel|US|Saudi_Arabia|UAE|Jordan|Iraq|Yemen|unknown",
  "proxy_target_type": "military|naval_vessel|civilian|infrastructure|unknown",
  "proxy_target_location": "<location or unknown>",
  "proxy_casualties": "<numeric string or null>",
  "proxy_iran_direction_claimed": "true|false",
  "proxy_escalation_indicators": ["<short phrase>"]
}

Rules:
- Use "unknown" for any field not clearly stated
- "proxy_iran_direction_claimed": "true" only if the text explicitly states Iran ordered or directed the operation
- "proxy_casualties": number mentioned; "null" if not mentioned
- "proxy_escalation_indicators": short phrases signaling broader conflict; empty array [] if none
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
🕸️ <b>Proxy Force Operation Detected</b>

<b>Group:</b> ${proxy_group}
<b>Operation:</b> ${proxy_operation_type}
<b>Target:</b> ${proxy_target_country} — ${proxy_target_location} (${proxy_target_type})
<b>Casualties:</b> ${proxy_casualties}
<b>Iran-directed:</b> ${proxy_iran_direction_claimed | yesno}
<b>Escalation signals:</b> ${proxy_escalation_indicators | join}

<blockquote expandable>${text_short}</blockquote>

🔗 <a href="${url}">Open source</a>

#proxy #${proxy_group} #${proxy_target_country}
```
