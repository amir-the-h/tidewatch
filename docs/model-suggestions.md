# Model Suggestions

## Currently Registered Models

| ID | Name | Type | Path | Active |
|----|------|------|------|--------|
| 1 | sentiment | sentiment | `/models/sentiment/model.onnx` | yes |
| 2 | emotion | classification | `/models/emotion/model.onnx` | yes |
| 3 | ner | ner | `/models/ner/model.onnx` | yes |
| 4 | zeroshot | classification | `/models/zeroshot/onnx/model_O4.onnx` | yes |

---

## Suggested Additions

The existing models cover: English sentiment, English emotion, English NER, and zero-shot classification.
The gaps are: **Persian/Farsi language support**, **tweet-native topic classification**, and **toxicity filtering**.

---

### 1. Persian NER
**`HooshvareLab/bert-fa-zwnj-base-ner`**

- **Type:** `ner`
- **Downloads:** 1.8M
- **License:** MIT
- **Why:** The existing `ner` model almost certainly covers English only. For non-English monitoring (e.g. Farsi or Arabic sources) a multilingual model is needed. This model extracts PER, ORG, and LOC entities from Persian text with proper ZWNJ normalization (critical for Farsi tokenization).
- **ONNX:** Export needed — `optimum-cli export onnx --model HooshvareLab/bert-fa-zwnj-base-ner --task token-classification ./models/fa-ner`
- **Suggested registration:**
  ```json
  { "name": "fa-ner", "version": "1.0", "type": "ner", "path": "/models/fa-ner/model.onnx" }
  ```

---

### 2. Twitter Topic Classification
**`cardiffnlp/tweet-topic-21-multi`**

- **Type:** `classification`
- **Downloads:** 2.0M
- **License:** Apache-2.0
- **Why:** Unlike the existing `emotion` model (which classifies emotional tone), this classifies the *subject* of a tweet across 21 categories including `politics`, `news & social concern`, `sports`, `science`, etc. It is trained directly on tweet data — not news articles or general text — making it a strong fit for this project.
- **Limitation:** English tweets only. For Persian tweets, fall back to the `zeroshot` model with topic labels.
- **ONNX:** Export needed — `optimum-cli export onnx --model cardiffnlp/tweet-topic-21-multi --task text-classification ./models/tweet-topic`
- **Suggested registration:**
  ```json
  { "name": "tweet-topic", "version": "1.0", "type": "classification", "path": "/models/tweet-topic/model.onnx" }
  ```
- **Labels (21):** `arts_&_culture`, `business_&_entrepreneurs`, `celebrity_&_pop_culture`, `diaries_&_daily_life`, `family`, `fashion_&_style`, `film_tv_&_video`, `fitness_&_health`, `food_&_dining`, `gaming`, `learning_&_educational`, `music`, `news_&_social_concern`, `politics`, `relationships`, `science_&_technology`, `sports`, `travel_&_adventure`, `youth_&_student_life`

---

### 3. Multilingual Toxicity Detection
**`unitary/multilingual-toxic-xlm-roberta`**

- **Type:** `classification`
- **Downloads:** 312K
- **License:** Apache-2.0
- **Why:** No current model detects toxic or abusive content. This is useful for filtering noise from alerts and flagging inflammatory content specifically. XLM-RoBERTa handles both English and Persian cross-lingually without a separate Farsi model.
- **ONNX:** Export needed — `optimum-cli export onnx --model unitary/multilingual-toxic-xlm-roberta --task text-classification ./models/toxicity`
- **Suggested registration:**
  ```json
  { "name": "toxicity", "version": "1.0", "type": "classification", "path": "/models/toxicity/model.onnx" }
  ```

---

### 4. Persian Sentiment (optional — if existing sentiment is English-only)
**`HooshvareLab/bert-fa-base-uncased-sentiment-digikala`**

- **Type:** `sentiment`
- **Downloads:** 382K
- **License:** Apache-2.0
- **Why:** Only add this if the current `sentiment` model does not handle Persian. This model was trained on Persian reviews — the domain differs from political tweets, so accuracy will be lower than ideal, but it is the best available Persian sentiment model on the Hub without fine-tuning.
- **ONNX:** Export needed — `optimum-cli export onnx --model HooshvareLab/bert-fa-base-uncased-sentiment-digikala --task text-classification ./models/fa-sentiment`
- **Suggested registration:**
  ```json
  { "name": "fa-sentiment", "version": "1.0", "type": "sentiment", "path": "/models/fa-sentiment/model.onnx" }
  ```

---

## ONNX Export — One-liner per model

```bash
# Requires: pip install optimum[exporters]

# Persian NER
optimum-cli export onnx \
  --model HooshvareLab/bert-fa-zwnj-base-ner \
  --task token-classification \
  ./models/fa-ner

# Tweet topic classification
optimum-cli export onnx \
  --model cardiffnlp/tweet-topic-21-multi \
  --task text-classification \
  ./models/tweet-topic

# Multilingual toxicity
optimum-cli export onnx \
  --model unitary/multilingual-toxic-xlm-roberta \
  --task text-classification \
  ./models/toxicity

# Persian sentiment (only if needed)
optimum-cli export onnx \
  --model HooshvareLab/bert-fa-base-uncased-sentiment-digikala \
  --task text-classification \
  ./models/fa-sentiment
```

---

## Priority Order

1. **tweet-topic** — highest signal value; complements zero-shot with fixed fast labels
2. **fa-ner** — most unique capability gap (no current Persian entity extraction)
3. **toxicity** — alert quality improvement; low effort
4. **fa-sentiment** — lowest priority; verify current `sentiment` model language coverage first
