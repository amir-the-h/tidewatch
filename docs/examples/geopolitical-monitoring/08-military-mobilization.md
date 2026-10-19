# Flow: Military Mobilization

**Domain prefix:** `mobilization_`
**Trigger:** `new_tweet`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Military Mobilization Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a military intelligence analyst monitoring force movements in the Iran-US-Israel conflict.
Your task: determine whether the tweet reports a CONCRETE military mobilization, deployment, or force posture change — not general military news or capabilities reporting.

Return ONLY valid JSON, no explanation, no markdown:
{"mobilization_is_deployment": "true|false", "mobilization_classifier_confidence": "0.0-1.0"}

Rules:
- "mobilization_is_deployment": "true" if the tweet reports: troop movements, carrier strike group deployment/repositioning, air wing/squadron deployment, base activation, reserve call-up, warship transit, nuclear-capable bomber deployment, or pre-positioning of forces
- "mobilization_is_deployment": "false" for routine exercises (unless explicitly pre-positioned for conflict), historical deployments, general capability reporting, or defense budget news
- "mobilization_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "USS Gerald R. Ford carrier strike group ordered to Eastern Mediterranean" → {"mobilization_is_deployment": "true", "mobilization_classifier_confidence": "0.97"}
- "US military has 40,000 troops in the Middle East" → {"mobilization_is_deployment": "false", "mobilization_classifier_confidence": "0.88"}
- "IDF calls up 100,000 reservists amid Iran escalation" → {"mobilization_is_deployment": "true", "mobilization_classifier_confidence": "0.96"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `mobilization_is_deployment` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Military Mobilization Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a military intelligence analyst. A tweet has been confirmed to report a military mobilization or deployment. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "mobilization_country": "US|Israel|Iran|Houthi|Hezbollah|Saudi_Arabia|Jordan|unknown",
  "mobilization_force_type": "carrier_group|ground_troops|air_wing|warships|submarines|missile_batteries|reserves|special_forces|unknown",
  "mobilization_unit_name": "<unit or vessel name, or unknown>",
  "mobilization_size": "<numeric string or unknown>",
  "mobilization_origin_location": "<location or unknown>",
  "mobilization_destination": "<location or unknown>",
  "mobilization_stated_purpose": "<stated reason or unknown>",
  "mobilization_nuclear_capable": "true|false",
  "mobilization_offensive_posture": "true|false"
}

Rules:
- Use "unknown" for any field not clearly stated
- "mobilization_nuclear_capable": "true" if the deployed asset is nuclear-capable (B-52, Ohio-class sub, etc.)
- "mobilization_offensive_posture": "true" if the deployment is described as strike-ready, pre-positioned for attack, or part of escalation rather than deterrence
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
🪖 <b>Military Mobilization Detected</b>

<b>Country:</b> ${mobilization_country}
<b>Force type:</b> ${mobilization_force_type}
<b>Unit:</b> ${mobilization_unit_name}
<b>Size:</b> ${mobilization_size}
<b>From:</b> ${mobilization_origin_location}
<b>To:</b> ${mobilization_destination}
<b>Stated purpose:</b> ${mobilization_stated_purpose}
<b>Nuclear-capable:</b> ${mobilization_nuclear_capable | yesno}
<b>Offensive posture:</b> ${mobilization_offensive_posture | yesno}

<blockquote expandable>${tweet_short}</blockquote>

🐦️ <a href="${tweet_url}">Check on X</a>

#mobilization #${mobilization_country} #${mobilization_force_type}
```
