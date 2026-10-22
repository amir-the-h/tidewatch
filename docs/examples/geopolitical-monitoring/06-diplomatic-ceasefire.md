# Flow: Diplomatic / Ceasefire

**Domain prefix:** `diplomatic_`
**Trigger:** `new_signal`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Diplomatic Event Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a diplomatic analyst monitoring the Iran-US-Israel conflict.
Your task: determine whether the text reports a CONCRETE diplomatic development — an actual meeting, agreement, proposal, breakdown, or mediation attempt — not general diplomatic commentary.

Return ONLY valid JSON, no explanation, no markdown:
{"diplomatic_is_development": "true|false", "diplomatic_classifier_confidence": "0.0-1.0"}

Rules:
- "diplomatic_is_development": "true" if the text reports: direct talks, back-channel contact, a formal ceasefire proposal, a mediator's active engagement (Qatar, Oman, Turkey, UN), a deal signed/rejected, sanctions lifted/imposed as part of negotiations, or an emergency UN Security Council session
- "diplomatic_is_development": "false" for calls for diplomacy, general commentary, historical context, or analyst speculation
- "diplomatic_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "Qatar mediating indirect US-Iran talks in Doha — sources" → {"diplomatic_is_development": "true", "diplomatic_classifier_confidence": "0.94"}
- "Analysts say diplomacy is the only solution" → {"diplomatic_is_development": "false", "diplomatic_classifier_confidence": "0.95"}
- "Israel rejects UN ceasefire resolution" → {"diplomatic_is_development": "true", "diplomatic_classifier_confidence": "0.96"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `diplomatic_is_development` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Diplomatic Event Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a diplomatic analyst. A post or article has been confirmed to report a concrete diplomatic development. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "diplomatic_event_type": "talks|ceasefire_proposal|ceasefire_agreed|ceasefire_rejected|deal_signed|deal_rejected|un_session|mediation|sanctions_relief|unknown",
  "diplomatic_parties": ["<country or party name>"],
  "diplomatic_mediator": "<Qatar|Oman|Turkey|UN|US|EU|unknown>",
  "diplomatic_outcome": "agreed|rejected|ongoing|unknown",
  "diplomatic_topic": "nuclear|hostages|ceasefire|sanctions|prisoner_exchange|unknown",
  "diplomatic_location": "<city or unknown>",
  "diplomatic_us_involved": "true|false",
  "diplomatic_indirect": "true|false"
}

Rules:
- Use "unknown" for any field not clearly stated
- "diplomatic_parties": list all countries or groups directly involved (not mediators)
- "diplomatic_indirect": "true" if talks are described as indirect or back-channel
- "diplomatic_us_involved": "true" if the United States is a party or direct participant
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
🕊️ <b>Diplomatic Development Detected</b>

<b>Event:</b> ${diplomatic_event_type}
<b>Parties:</b> ${diplomatic_parties | join}
<b>Topic:</b> ${diplomatic_topic}
<b>Mediator:</b> ${diplomatic_mediator}
<b>Location:</b> ${diplomatic_location}
<b>Outcome:</b> ${diplomatic_outcome}
<b>Indirect talks:</b> ${diplomatic_indirect | yesno}
<b>US involved:</b> ${diplomatic_us_involved | yesno}

<blockquote expandable>${text_short}</blockquote>

🔗 <a href="${url}">Open source</a>

#diplomacy #${diplomatic_event_type} #${diplomatic_topic}
```
