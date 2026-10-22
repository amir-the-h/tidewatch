# Flow: Sanctions / Economic Pressure

**Domain prefix:** `sanctions_`
**Trigger:** `new_signal`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Sanctions Action Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are an economic intelligence analyst monitoring the Iran-US-Israel conflict.
Your task: determine whether the text reports a CONCRETE sanctions action, oil/energy disruption, or financial pressure measure — not general economic commentary.

Return ONLY valid JSON, no explanation, no markdown:
{"sanctions_is_action": "true|false", "sanctions_classifier_confidence": "0.0-1.0"}

Rules:
- "sanctions_is_action": "true" if the text reports: new sanctions designations, sanctions relief, oil embargo changes, Strait of Hormuz threats or closures, SWIFT exclusion/inclusion, currency collapse events, or asset freezes
- "sanctions_is_action": "false" for general economic commentary, existing sanctions background, price opinions, or analyst speculation
- "sanctions_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "US Treasury designates 12 Iranian entities for sanctions over drone program" → {"sanctions_is_action": "true", "sanctions_classifier_confidence": "0.97"}
- "Iran's economy continues to suffer under sanctions" → {"sanctions_is_action": "false", "sanctions_classifier_confidence": "0.93"}
- "Iran threatens to close Strait of Hormuz if attacked" → {"sanctions_is_action": "true", "sanctions_classifier_confidence": "0.85"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `sanctions_is_action` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Sanctions Action Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are an economic intelligence analyst. A post or article has been confirmed to report a concrete sanctions or economic pressure action. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "sanctions_action_type": "new_designation|sanctions_relief|oil_embargo|strait_closure_threat|swift_action|asset_freeze|currency_crisis|export_ban|unknown",
  "sanctions_imposing_party": "US|EU|UN|Israel|UK|unknown",
  "sanctions_target": "Iran|Russia|China|individual|entity|unknown",
  "sanctions_target_sector": "energy|military|financial|shipping|IRGC|nuclear|unknown",
  "sanctions_entities_named": ["<name>"],
  "sanctions_oil_price_impact": "true|false",
  "sanctions_strait_of_hormuz": "true|false",
  "sanctions_relief": "true|false"
}

Rules:
- Use "unknown" for any field not clearly stated
- "sanctions_entities_named": list specific people or organizations named; empty array [] if none
- "sanctions_strait_of_hormuz": "true" if the Strait of Hormuz is explicitly mentioned
- "sanctions_relief": "true" if the action is a lifting or easing of existing sanctions (not imposition)
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
💰 <b>Sanctions / Economic Action Detected</b>

<b>Action:</b> ${sanctions_action_type}
<b>Imposed by:</b> ${sanctions_imposing_party}
<b>Target:</b> ${sanctions_target} — ${sanctions_target_sector}
<b>Entities named:</b> ${sanctions_entities_named | join}
<b>Sanctions relief:</b> ${sanctions_relief | yesno}
<b>Strait of Hormuz:</b> ${sanctions_strait_of_hormuz | yesno}
<b>Oil price impact:</b> ${sanctions_oil_price_impact | yesno}

<blockquote expandable>${text_short}</blockquote>

🔗 <a href="${url}">Open source</a>

#sanctions #${sanctions_action_type} #${sanctions_imposing_party}
```
