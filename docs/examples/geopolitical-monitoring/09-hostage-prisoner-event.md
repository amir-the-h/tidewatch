# Flow: Hostage / Prisoner Event

**Domain prefix:** `hostage_`
**Trigger:** `new_tweet`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Hostage Event Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are an intelligence analyst monitoring hostage and prisoner situations involving Iran and its adversaries.
Your task: determine whether the tweet reports a CONCRETE hostage, detention, or prisoner exchange event — not background reporting or general commentary.

Return ONLY valid JSON, no explanation, no markdown:
{"hostage_is_event": "true|false", "hostage_classifier_confidence": "0.0-1.0"}

Rules:
- "hostage_is_event": "true" if the tweet reports: a new detention/arrest of a foreign national or dual citizen, a prisoner exchange or release, hostage negotiations, a trial verdict for a detained foreigner, or a death in custody
- "hostage_is_event": "false" for background stories about long-held prisoners, advocacy without new developments, or general commentary on Iran's hostage-taking policy
- "hostage_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "Iran releases 5 American prisoners in exchange for $6B in frozen funds" → {"hostage_is_event": "true", "hostage_classifier_confidence": "0.98"}
- "Iran holds more dual nationals than any other country" → {"hostage_is_event": "false", "hostage_classifier_confidence": "0.94"}
- "Iran sentences Swedish-Iranian journalist to death" → {"hostage_is_event": "true", "hostage_classifier_confidence": "0.96"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `hostage_is_event` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Hostage Event Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are an intelligence analyst. A tweet has been confirmed to report a hostage or prisoner event. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "hostage_event_type": "new_arrest|prisoner_exchange|release|trial|sentencing|death_in_custody|negotiations|unknown",
  "hostage_detaining_party": "Iran|Houthi|Hezbollah|Hamas|Israel|US|unknown",
  "hostage_nationality": "<country of citizenship or unknown>",
  "hostage_names": ["<name>"],
  "hostage_count": "<numeric string or unknown>",
  "hostage_charges": "<stated charges or unknown>",
  "hostage_sentence": "<sentence if applicable or unknown>",
  "hostage_exchange_terms": "<terms if exchange or unknown>",
  "hostage_dual_national": "true|false|unknown"
}

Rules:
- Use "unknown" for any field not clearly stated
- "hostage_names": list full names if mentioned; empty array [] if not named
- "hostage_dual_national": "true" if the person holds dual citizenship
- "hostage_exchange_terms": describe assets, frozen funds, or prisoners exchanged if mentioned
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
🔒 <b>Hostage / Prisoner Event Detected</b>

<b>Event:</b> ${hostage_event_type}
<b>Detaining party:</b> ${hostage_detaining_party}
<b>Nationality:</b> ${hostage_nationality}
<b>Names:</b> ${hostage_names | join}
<b>Count:</b> ${hostage_count}
<b>Charges:</b> ${hostage_charges}
<b>Sentence:</b> ${hostage_sentence}
<b>Exchange terms:</b> ${hostage_exchange_terms}
<b>Dual national:</b> ${hostage_dual_national | yesno}

<blockquote expandable>${tweet_short}</blockquote>

🐦️ <a href="${tweet_url}">Check on X</a>

#hostage #${hostage_detaining_party} #${hostage_nationality}
```
