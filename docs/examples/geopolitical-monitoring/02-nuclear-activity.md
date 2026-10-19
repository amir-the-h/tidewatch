# Flow: Nuclear Activity

**Domain prefix:** `nuclear_`
**Trigger:** `new_tweet`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Nuclear Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a nuclear nonproliferation analyst monitoring Iran's nuclear program.
Your task: determine whether the tweet reports a CONCRETE development in Iran's nuclear activities — not opinion or background reporting.

Return ONLY valid JSON, no explanation, no markdown:
{"nuclear_is_development": "true|false", "nuclear_classifier_confidence": "0.0-1.0"}

Rules:
- "nuclear_is_development": "true" if the tweet reports enrichment level changes, new centrifuge installation, IAEA access granted/denied, stockpile updates, facility damage/attack, breakout timeline estimates from official sources, or nuclear deal status changes
- "nuclear_is_development": "false" for general commentary, opinion, historical background, threats with no concrete development, or diplomatic rhetoric
- "nuclear_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "Iran now enriching uranium to 84%, IAEA says" → {"nuclear_is_development": "true", "nuclear_classifier_confidence": "0.97"}
- "Biden calls Iran's nuclear program a threat" → {"nuclear_is_development": "false", "nuclear_classifier_confidence": "0.93"}
- "IAEA inspectors denied access to Fordow facility" → {"nuclear_is_development": "true", "nuclear_classifier_confidence": "0.95"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `nuclear_is_development` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Nuclear Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a nuclear nonproliferation analyst. A tweet has been confirmed to report a concrete Iran nuclear development. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "nuclear_event_type": "enrichment_change|centrifuge_install|iaea_access|stockpile_update|facility_attack|deal_status|breakout_estimate|unknown",
  "nuclear_facility": "Natanz|Fordow|Arak|Isfahan|Bushehr|unknown",
  "nuclear_enrichment_level": "<percentage string or unknown>",
  "nuclear_iaea_involved": "true|false",
  "nuclear_iaea_access": "granted|denied|partial|unknown",
  "nuclear_breakout_timeline": "<string e.g. '1 week' or unknown>",
  "nuclear_source": "<organization or person name, or unknown>",
  "nuclear_deal_status": "active|collapsed|negotiating|unknown",
  "nuclear_significance": "high|medium|low"
}

Rules:
- Use "unknown" for any field not clearly stated
- "nuclear_significance": "high" if enrichment >60%, IAEA access denied, or facility attacked; "medium" for other confirmed developments; "low" for status updates without new thresholds
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
☢️ <b>Nuclear Development Detected</b>

<b>Event:</b> ${nuclear_event_type}
<b>Facility:</b> ${nuclear_facility}
<b>Enrichment level:</b> ${nuclear_enrichment_level}
<b>IAEA involved:</b> ${nuclear_iaea_involved | yesno}
<b>IAEA access:</b> ${nuclear_iaea_access}
<b>Breakout timeline:</b> ${nuclear_breakout_timeline}
<b>Deal status:</b> ${nuclear_deal_status}
<b>Significance:</b> ${nuclear_significance}
<b>Source:</b> ${nuclear_source}

<blockquote expandable>${tweet_short}</blockquote>

🐦️ <a href="${tweet_url}">Check on X</a>

#nuclear #Iran #${nuclear_facility}
```
