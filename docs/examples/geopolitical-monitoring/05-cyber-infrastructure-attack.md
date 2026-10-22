# Flow: Cyber / Infrastructure Attack

**Domain prefix:** `cyber_`
**Trigger:** `new_signal`
**Chain pattern:** Detector → Condition → Extractor → Notify

---

## Detector Pipeline

**Name:** Cyber Attack Detector
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a cybersecurity intelligence analyst monitoring Iran-US-Israel conflict.
Your task: determine whether the text reports a CONFIRMED or CREDIBLY CLAIMED cyberattack or infrastructure sabotage related to the conflict.

Return ONLY valid JSON, no explanation, no markdown:
{"cyber_is_attack": "true|false", "cyber_classifier_confidence": "0.0-1.0"}

Rules:
- "cyber_is_attack": "true" if the text reports a confirmed or claimed cyber intrusion, data breach, ransomware, SCADA/ICS attack, power grid disruption, port/refinery disruption, or physical sabotage of infrastructure
- "cyber_is_attack": "false" for general warnings, capability announcements, historical references, sanctions for past cyber activity, or unconfirmed rumors
- "cyber_classifier_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "Iran-linked hackers breach Israeli water authority systems" → {"cyber_is_attack": "true", "cyber_classifier_confidence": "0.95"}
- "US warns of increased Iranian cyber threat to critical infrastructure" → {"cyber_is_attack": "false", "cyber_classifier_confidence": "0.92"}
- "Power grid in Isfahan goes dark amid reports of explosion at substation" → {"cyber_is_attack": "true", "cyber_classifier_confidence": "0.88"}
```

---

## Condition Node

| Key | Operator | Value |
|---|---|---|
| `cyber_is_attack` | `==` | `"true"` |

---

## Extractor Pipeline

**Name:** Cyber Attack Extractor
**Type:** `ollama`
**Model:** `qwen2.5:7b-instruct`
**Usecase:** `custom`

**System prompt:**
```
You are a cybersecurity intelligence analyst. A post or article has been confirmed to report a cyberattack or infrastructure sabotage. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "cyber_attack_type": "intrusion|data_breach|ransomware|SCADA_ICS|DDoS|physical_sabotage|wiper|unknown",
  "cyber_attacker": "Iran|Israel|US|unknown",
  "cyber_attacker_group": "<group name or unknown>",
  "cyber_target_country": "Iran|Israel|US|Saudi_Arabia|Iraq|unknown",
  "cyber_target_sector": "energy|water|financial|military|government|telecoms|nuclear|port|unknown",
  "cyber_target_name": "<specific facility, agency, or company, or unknown>",
  "cyber_operational_impact": "true|false",
  "cyber_data_exfiltrated": "true|false|unknown",
  "cyber_attribution_official": "true|false"
}

Rules:
- Use "unknown" for any field not clearly stated
- "cyber_operational_impact": "true" if the attack caused actual disruption (outage, explosion, shutdown), not just intrusion
- "cyber_attribution_official": "true" only if a government or official security body made the attribution
```

---

## Notification Template

**Handler:** `notification_tg`
**Parse mode:** `HTML`

```
💻 <b>Cyber / Infrastructure Attack Detected</b>

<b>Type:</b> ${cyber_attack_type}
<b>Attacker:</b> ${cyber_attacker} — ${cyber_attacker_group}
<b>Target:</b> ${cyber_target_country} — ${cyber_target_name} (${cyber_target_sector})
<b>Operational impact:</b> ${cyber_operational_impact | yesno}
<b>Data exfiltrated:</b> ${cyber_data_exfiltrated | yesno}
<b>Official attribution:</b> ${cyber_attribution_official | yesno}

<blockquote expandable>${text_short}</blockquote>

🔗 <a href="${url}">Open source</a>

#cyber #${cyber_attack_type} #${cyber_target_country}
```
