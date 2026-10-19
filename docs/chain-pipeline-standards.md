# Chain Pipeline Standards

Standards for building Ollama pipelines and notification templates in tidewatch chains.

## 1. Output Key Naming

All Ollama pipeline output keys **must be prefixed** with the chain's domain name. This prevents collisions when multiple chains run on the same tweet and their outputs are merged into a shared context.

**Pattern:** `{domain}_{key}`

| Chain domain | Example keys |
|---|---|
| `assassination` | `assassination_is_assassination`, `assassination_confidence` |
| `kinetic` | `kinetic_is_attack`, `kinetic_confidence`, `kinetic_attacker` |
| `diplomatic` | `diplomatic_is_event`, `diplomatic_confidence` |

Never use bare generic names like `confidence`, `label`, `matched`, `is_event` — these will silently overwrite or be overwritten by other chains.

## 2. Detection Pipeline

The first pipeline in a chain is a binary classifier that determines whether the tweet is relevant to the chain's domain.

**Output schema:**
```json
{
  "{domain}_is_{event}": "true|false",
  "{domain}_classifier_confidence": "0.0-1.0"
}
```

Rules:
- Both values must be **quoted strings**, not JSON booleans or numbers
- `{domain}_is_{event}` drives the downstream condition node
- `{domain}_classifier_confidence` is available for optional confidence-threshold conditions
- The `_classifier_confidence` suffix distinguishes the detection confidence from any extractor confidence field

**Prompt structure:**
```
You are an intelligence analyst monitoring [topic] news.
Your task: determine whether the tweet reports [specific event definition].

Return ONLY valid JSON, no explanation, no markdown:
{"[domain]_is_[event]": "true|false", "[domain]_confidence": "0.0-1.0"}

Rules:
- "[domain]_is_[event]": "true" only if [precise positive criteria]
- "[domain]_is_[event]": "false" for [explicit negative cases]
- "[domain]_confidence": your certainty as a decimal string between "0.0" and "1.0"

Examples:
- "[negative example tweet]" → {"[domain]_is_[event]": "false", "[domain]_confidence": "0.97"}
- "[positive example tweet]" → {"[domain]_is_[event]": "true", "[domain]_confidence": "0.95"}
```

## 3. Extraction Pipeline

The second pipeline runs only on tweets that passed the detection condition. It extracts structured intelligence.

**Output schema:**
```json
{
  "{domain}_{field1}": "<value or null>",
  "{domain}_{field2}": "<value or null>",
  "{domain}_{array_field}": ["item1", "item2"]
}
```

Rules:
- All keys prefixed with domain name
- Use `"null"` (string) for unknown scalar fields, `[]` for unknown array fields
- Array fields (e.g., indicators, tags) use JSON arrays — the `join` template filter will flatten them for display
- Boolean-like fields (e.g., `response_mentioned`) use `"true"/"false"` strings

**Prompt structure:**
```
You are an intelligence analyst. A tweet has been confirmed to report [event type]. Extract all available details.

Return ONLY valid JSON, no explanation, no markdown:
{
  "[domain]_[field1]": "<description or unknown>",
  "[domain]_[field2]": "option1|option2|unknown",
  "[domain]_[bool_field]": "true|false",
  "[domain]_[array_field]": ["<item>"]
}

Rules:
- Use "unknown" for any field not clearly stated in the tweet
- [field-specific rules...]
- "[domain]_[bool_field]": "true" if [condition]; otherwise "false"
- "[domain]_[array_field]": short phrases for [description]; empty array [] if none
```

## 4. Condition Node

The condition node between detection and extraction checks the detection output.

**Standard condition:**

| Key | Operator | Value |
|---|---|---|
| `{domain}_is_{event}` | `==` | `"true"` |

Optional secondary condition for confidence threshold:

| Key | Operator | Value |
|---|---|---|
| `{domain}_classifier_confidence` | `>=` | `"0.7"` |

## 5. Notification Templates

### Parse mode

Always use **HTML** parse mode. MarkdownV2 requires escaping too many characters that appear in tweet text and extracted values.

### Tweet content

Always use `${tweet_short}` (truncated to 280 chars) inside `<blockquote expandable>` — never `${tweet}` directly, as long tweets exceed Telegram's 4096-char message limit.

### Template filters

Use `${key | filter}` syntax for human-readable display of raw values:

| Filter | Input | Output |
|---|---|---|
| `yesno` | `"true"` / `"false"` / `true` / `false` | `"yes"` / `"no"` |
| `join` | `["a","b"]` / `[]` | `"a, b"` / `` (line dropped) |

Empty lines (where substituted value is empty or join produced nothing) are automatically dropped — no need for conditional logic in the template.

### Template structure

```
{emoji} <b>{Chain Name} Detected</b>

<b>Field 1:</b> ${domain_field1}
<b>Field 2:</b> ${domain_field2} (${domain_field3})
<b>Boolean:</b> ${domain_bool_field | yesno}
<b>Array:</b> ${domain_array_field | join}

<blockquote expandable>${tweet_short}</blockquote>

🐦️ <a href="${tweet_url}">Check on X</a>

#domain #${domain_category_field} #${domain_actor_field}
```

Lines containing only a label and an empty value are dropped automatically, including HTML-wrapped labels like `<b>Field:</b> `.

### Channel config

```json
{
  "parse_mode": "HTML",
  "chat_id": "..."
}
```
