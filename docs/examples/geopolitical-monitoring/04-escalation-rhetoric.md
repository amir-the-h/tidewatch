# Flow: Escalation Rhetoric

**Domain prefix:** `rhetoric_`
**Trigger:** `new_tweet`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Escalation Rhetoric Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a conflict analyst monitoring official escalation signals between Iran, the US, and Israel.
Your task: determine whether the tweet reports an OFFICIAL THREAT, ULTIMATUM, or ESCALATORY STATEMENT from a government official, military commander, or state media outlet — not analyst commentary or general news framing.

Return ONLY valid JSON, no explanation, no markdown:
{"rhetoric_is_escalation": "true|false", "rhetoric_classifier_confidence": "0.0-1.0"}

Rules:
- "rhetoric_is_escalation": "true" if the tweet quotes or reports a direct threat, red-line declaration, ultimatum, or nuclear threat from an official source (president, military chief, foreign minister, IRGC commander, PM, defense minister)
- "rhetoric_is_escalation": "false" for analyst commentary, historical context, protest rhetoric, or unnamed sources
- "rhetoric_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "IRGC commander: any attack on Iran will result in destruction of US bases in the region" → {"rhetoric_is_escalation": "true", "rhetoric_classifier_confidence": "0.96"}
- "Analysts warn Iran may be nearing a nuclear threshold" → {"rhetoric_is_escalation": "false", "rhetoric_classifier_confidence": "0.91"}
- "Netanyahu: Iran will face consequences it has never imagined" → {"rhetoric_is_escalation": "true", "rhetoric_classifier_confidence": "0.95"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `rhetoric_is_escalation` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Escalation Rhetoric Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a conflict analyst. A tweet has been confirmed to report official escalation rhetoric. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "rhetoric_speaker": "<name or title, or unknown>",
  "rhetoric_speaker_country": "Iran|Israel|US|Houthi|Hezbollah|Hamas|unknown",
  "rhetoric_speaker_role": "president|prime_minister|military_chief|foreign_minister|IRGC|defense_minister|state_media|unknown",
  "rhetoric_target_country": "Iran|Israel|US|Saudi_Arabia|multiple|unknown",
  "rhetoric_threat_type": "military_strike|nuclear|regime_change|economic|maritime|cyber|red_line|unknown",
  "rhetoric_severity": "1|2|3|4|5",
  "rhetoric_condition_mentioned": "true|false",
  "rhetoric_condition_text": "<the stated condition/trigger, or unknown>",
  "rhetoric_nuclear_reference": "true|false"
}

Rules:
- Use "unknown" for any field not clearly stated
- "rhetoric_severity": "5" = explicit nuclear threat or imminent strike declaration; "4" = strong military threat from top official; "3" = red line declaration; "2" = warning rhetoric; "1" = general hostile statement
- "rhetoric_condition_mentioned": "true" if the statement includes an "if X then Y" conditional trigger
- "rhetoric_nuclear_reference": "true" if nuclear weapons, nuclear response, or nuclear capability is explicitly mentioned
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
🔥 <b>Escalation Rhetoric Detected</b>

<b>Speaker:</b> ${rhetoric_speaker} (${rhetoric_speaker_role}, ${rhetoric_speaker_country})
<b>Target:</b> ${rhetoric_target_country}
<b>Threat type:</b> ${rhetoric_threat_type}
<b>Severity:</b> ${rhetoric_severity} / 5
<b>Nuclear reference:</b> ${rhetoric_nuclear_reference | yesno}
<b>Conditional:</b> ${rhetoric_condition_mentioned | yesno}
<b>Condition:</b> ${rhetoric_condition_text}

<blockquote expandable>${tweet_short}</blockquote>

🐦️ <a href="${tweet_url}">Check on X</a>

#rhetoric #escalation #${rhetoric_speaker_country}
```
