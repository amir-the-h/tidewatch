# Flow: Casualty Report

**Domain prefix:** `casualty_`
**Trigger:** `new_signal`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Casualty Report Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a conflict analyst monitoring the Iran-US-Israel war.
Your task: determine whether the text reports CONFIRMED or OFFICIALLY CLAIMED casualty figures from a conflict event — not estimates, projections, or historical totals.

Return ONLY valid JSON, no explanation, no markdown:
{"casualty_is_report": "true|false", "casualty_classifier_confidence": "0.0-1.0"}

Rules:
- "casualty_is_report": "true" if the text provides specific casualty numbers (killed, wounded, missing) attributed to a named event, strike, or operation, from an official or credible source
- "casualty_is_report": "false" for cumulative war totals without a specific event, historical comparisons, projections, medical system capacity reports, or unverified social media claims without sourcing
- "casualty_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "IDF says 3 soldiers killed in Hezbollah anti-tank missile strike in northern Israel" → {"casualty_is_report": "true", "casualty_classifier_confidence": "0.97"}
- "Over 40,000 Palestinians killed since October 7, Gaza health ministry says" → {"casualty_is_report": "true", "casualty_classifier_confidence": "0.91"}
- "War analysts project 100,000 casualties if conflict expands" → {"casualty_is_report": "false", "casualty_classifier_confidence": "0.95"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `casualty_is_report` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Casualty Report Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a conflict analyst. A post or article has been confirmed to report casualty figures. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "casualty_event_name": "<brief description of the event or unknown>",
  "casualty_killed": "<numeric string or null>",
  "casualty_wounded": "<numeric string or null>",
  "casualty_missing": "<numeric string or null>",
  "casualty_side": "Iran|Israel|US|Houthi|Hezbollah|Hamas|civilian|multiple|unknown",
  "casualty_military_vs_civilian": "military|civilian|mixed|unknown",
  "casualty_location": "<location or unknown>",
  "casualty_source": "<organization, military, or official who reported, or unknown>",
  "casualty_source_reliability": "official|credible_media|unverified"
}

Rules:
- Use "null" for any count not mentioned (killed, wounded, missing)
- "casualty_source_reliability": "official" = government/military statement; "credible_media" = established outlet; "unverified" = social media or anonymous claim
- "casualty_side": use "civilian" if the victims are explicitly non-combatants; "multiple" if both sides reported
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
💀 <b>Casualty Report Detected</b>

<b>Event:</b> ${casualty_event_name}
<b>Side:</b> ${casualty_side} (${casualty_military_vs_civilian})
<b>Location:</b> ${casualty_location}
<b>Killed:</b> ${casualty_killed}
<b>Wounded:</b> ${casualty_wounded}
<b>Missing:</b> ${casualty_missing}
<b>Source:</b> ${casualty_source} (${casualty_source_reliability})

<blockquote expandable>${text_short}</blockquote>

🔗 <a href="${url}">Open source</a>

#casualties #${casualty_side} #${casualty_location}
```
