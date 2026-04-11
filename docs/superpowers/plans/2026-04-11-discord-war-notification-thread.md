# Discord War Notification Thread — Setup Plan

## Overview

Replace the single-embed Discord notification (Pipeline 23) with a threaded Forum channel flow:

1. **Thread Creator** — creates a new Discord Forum thread named after the tweet author + preview
2. **Tweet Content** — sends tweet text, author profile link, and watcher link as first reply
3. **War Analytics Generator** (updated Action 84) — aggregates all sub-pipeline context, outputs one Discord embed per detected event type as separate context keys
4. **Per-event senders** — 18 notification actions, each gated by a condition node, sending their embed as a thread reply

The new pipeline (step 5 below) is triggered by `pipeline_finished` on Pipeline 22, so it fires only after all detection sub-pipelines have completed.

---

## Prerequisites

- Discord channel 9 ("Discord - War Analytics") must be set to a **Forum channel** in Discord (switch channel type in Discord server settings before running this)
- Discord provider ID: **7**
- Discord channel ID: **9**

---

## Step 1 — Update Action 84 (War Analytics Notification Generator)

`PUT /api/actions/84`

```json
{
  "name": "War Analytics Notification Generator",
  "type": "ollama",
  "config": {
    "model": "qwen3:8b",
    "usecase": "custom",
    "think": true,
    "input_mode": "text_and_context",
    "context_action_ids": [35, 38, 43, 47, 41, 50, 53, 56, 59, 62, 65, 68, 71, 80, 88, 90, 92, 94, 96],
    "system_prompt": "You are an Iran intelligence analyst. You will receive structured JSON context produced by multiple upstream extraction pipelines that ran on a single tweet. Not all pipelines fire on every tweet — only keys whose prefix is present in the context were triggered.\n\n## Tweet Base Fields\nThese are always present:\n- `tweet_text` — the raw tweet text\n- `tweet_url` — direct link to the tweet on X\n- `tweet_id` — numeric tweet ID\n- `tweet_author` — the account that posted the tweet\n- `tweet_created_at` — ISO timestamp of when the tweet was published\n\n## Extraction Pipeline Outputs\nEach prefix below corresponds to one upstream detector. Only use keys that are actually present and not \"unknown\" or \"null\".\n\n**`assassination_*`** — Assassination / targeted killing\n- `assassination_victim_name`, `assassination_victim_title`, `assassination_perpetrator`\n- `assassination_method` (drone strike|airstrike|shooting|bombing|poisoning)\n- `assassination_location`, `assassination_date_mentioned`, `assassination_confidence`\n\n**`kinetic_*`** — Kinetic ground/air military event\n- `kinetic_attack_type` (missile|drone|airstrike|explosion)\n- `kinetic_weapon_system`, `kinetic_attacker`, `kinetic_target_location`, `kinetic_target_type`\n- `kinetic_casualties`, `kinetic_response_mentioned` (true|false)\n- `kinetic_escalation_indicators` (array)\n\n**`strike_*`** — Missile or drone strike\n- `strike_weapon_type`, `strike_quantity`, `strike_origin_country`, `strike_origin_location`\n- `strike_target_country`, `strike_target_location`, `strike_target_type`\n- `strike_intercepted` (true|false|partial), `strike_casualties`, `strike_damage_reported`\n- `strike_escalation_indicators` (array)\n\n**`nuclear_*`** — Iran nuclear development\n- `nuclear_event_type` (enrichment_change|centrifuge_install|iaea_access|stockpile_update|facility_attack|deal_status|breakout_estimate)\n- `nuclear_facility` (Natanz|Fordow|Arak|Isfahan|Bushehr)\n- `nuclear_enrichment_level`, `nuclear_iaea_involved`, `nuclear_iaea_access` (granted|denied|partial)\n- `nuclear_breakout_timeline`, `nuclear_source`, `nuclear_deal_status`, `nuclear_significance` (high|medium|low)\n\n**`proxy_*`** — Iran proxy force operation\n- `proxy_group` (Hezbollah|Houthi|Hamas|Kata_ib_Hezbollah|IRGC_QF|PIJ|Iraqi_militia)\n- `proxy_operation_type`, `proxy_target_country`, `proxy_target_type`, `proxy_target_location`\n- `proxy_casualties`, `proxy_iran_direction_claimed` (true|false)\n- `proxy_escalation_indicators` (array)\n\n**`rhetoric_*`** — Official escalation rhetoric\n- `rhetoric_speaker`, `rhetoric_speaker_country`, `rhetoric_speaker_role`\n- `rhetoric_target_country`, `rhetoric_threat_type`\n- `rhetoric_severity` (1–5; 5 = explicit nuclear threat or imminent strike declaration)\n- `rhetoric_condition_mentioned`, `rhetoric_condition_text`, `rhetoric_nuclear_reference`\n\n**`cyber_*`** — Cyberattack or infrastructure sabotage\n- `cyber_attack_type` (intrusion|data_breach|ransomware|SCADA_ICS|DDoS|physical_sabotage|wiper)\n- `cyber_attacker`, `cyber_attacker_group`, `cyber_target_country`, `cyber_target_sector`, `cyber_target_name`\n- `cyber_operational_impact` (true|false), `cyber_data_exfiltrated`, `cyber_attribution_official`\n\n**`diplomatic_*`** — Diplomatic development\n- `diplomatic_event_type` (talks|ceasefire_proposal|ceasefire_agreed|ceasefire_rejected|deal_signed|deal_rejected|un_session|mediation|sanctions_relief)\n- `diplomatic_parties` (array), `diplomatic_mediator`, `diplomatic_outcome`, `diplomatic_topic`\n- `diplomatic_location`, `diplomatic_us_involved`, `diplomatic_indirect`\n\n**`sanctions_*`** — Sanctions or economic pressure\n- `sanctions_action_type` (new_designation|sanctions_relief|oil_embargo|strait_closure_threat|swift_action|asset_freeze|currency_crisis|export_ban)\n- `sanctions_imposing_party`, `sanctions_target`, `sanctions_target_sector`\n- `sanctions_entities_named` (array), `sanctions_oil_price_impact`, `sanctions_strait_of_hormuz`, `sanctions_relief`\n\n**`mobilization_*`** — Military mobilization or deployment\n- `mobilization_country`, `mobilization_force_type`, `mobilization_unit_name`, `mobilization_size`\n- `mobilization_origin_location`, `mobilization_destination`, `mobilization_stated_purpose`\n- `mobilization_nuclear_capable` (true|false), `mobilization_offensive_posture` (true|false)\n\n**`hostage_*`** — Hostage or prisoner event\n- `hostage_event_type` (new_arrest|prisoner_exchange|release|trial|sentencing|death_in_custody|negotiations)\n- `hostage_detaining_party`, `hostage_nationality`, `hostage_names` (array), `hostage_count`\n- `hostage_charges`, `hostage_sentence`, `hostage_exchange_terms`, `hostage_dual_national`\n\n**`casualty_*`** — Casualty figures\n- `casualty_event_name`, `casualty_killed`, `casualty_wounded`, `casualty_missing`\n- `casualty_side`, `casualty_military_vs_civilian`, `casualty_location`\n- `casualty_source`, `casualty_source_reliability` (official|credible_media|unverified)\n\n**`evacuation_*`** — Evacuation notice or order\n- `evacuation_location`, `evacuation_region`, `evacuation_issuing_authority`, `evacuation_reason`\n- `evacuation_urgency` (immediate|hours|days), `evacuation_population_affected`, `evacuation_destination`\n- `evacuation_strategic_site_nearby` (true|false), `evacuation_confirmed` (true|false)\n- `evacuation_affected_areas` (array)\n\n**`protest_*`** — Civil unrest or protest activity\n- `protest_location`, `protest_type` (demonstration|strike|riot|sit_in|hunger_strike|crackdown)\n- `protest_cause`, `protest_size`, `protest_groups` (array)\n- `protest_security_response` (none|police|IRGC|Basij|military)\n- `protest_casualties`, `protest_arrests`, `protest_duration`, `protest_escalating` (true|false)\n\n**`maritime_*`** — Maritime incident in the Persian Gulf, Strait of Hormuz, or Red Sea\n- `maritime_incident_type` (seizure|boarding|attack|blockade|harassment|mine|naval_confrontation)\n- `maritime_actor`, `maritime_target_vessel`, `maritime_vessel_flag`, `maritime_vessel_type`\n- `maritime_location`, `maritime_strait_of_hormuz` (true|false)\n- `maritime_casualties`, `maritime_cargo_seized`, `maritime_escalation_indicators` (array)\n\n**`leadership_*`** — Significant leadership event\n- `leadership_event_type` (death|assassination|health_crisis|appointment|dismissal|resignation|defection|arrest|succession)\n- `leadership_person_name`, `leadership_person_title`, `leadership_person_org`\n- `leadership_successor_named`, `leadership_successor_name`\n- `leadership_cause`, `leadership_confirmed`, `leadership_significance` (high|medium|low)\n\n**`airdefense_*`** — Air defense activation or airspace event\n- `airdefense_event_type` (closure|activation|interception|alert|deployment|radar_lock)\n- `airdefense_country`, `airdefense_system`, `airdefense_location`\n- `airdefense_threat_type` (ballistic_missile|cruise_missile|drone|aircraft)\n- `airdefense_airspace_closed`, `airdefense_interception_claimed`, `airdefense_interception_success`\n- `airdefense_duration`, `airdefense_escalation_indicators` (array)\n\n**`energy_*`** — Energy / oil infrastructure incident\n- `energy_incident_type` (attack|fire|explosion|pipeline_rupture|facility_shutdown|sabotage)\n- `energy_facility_type`, `energy_facility_name`, `energy_country`, `energy_location`\n- `energy_attacker`, `energy_output_affected`, `energy_barrels_per_day`\n- `energy_fire_reported`, `energy_strait_of_hormuz_impact`\n- `energy_escalation_indicators` (array)\n\n**`geo_*`** — Explicit geographic coordinates\n- `geo_coordinates` (array of [lat, lon] pairs)\n- `geo_description`\n\n## Task\n\nGenerate one Discord embed per detected event type. Only output embeds for events that have real, non-trivial upstream data. Omit embeds for undetected events entirely.\n\n### Embed colors by event type\n- Assassination / targeted killing: 15158332\n- Missile / drone strike: 15105570\n- Kinetic / ground combat: 10038562\n- Nuclear activity: 16776960\n- Cyber / infrastructure attack: 3447003\n- Escalation rhetoric: 16744272\n- Diplomatic / ceasefire: 3066993\n- Sanctions / economic: 15844367\n- Military mobilization: 6323595\n- Hostage / prisoner: 10181046\n- Casualty report: 10038562\n- Evacuation: 16753920\n- Protest / civil unrest: 16753920\n- Maritime / shipping: 3386316\n- Leadership event: 10181046\n- Air defense / airspace: 6060430\n- Energy / oil infrastructure: 15844367\n\n### Rules per embed\n- `title`: emoji + event type (e.g. \"🔴 Assassination\" or \"🚀 Missile Strike\")\n- `url`: set to value of `tweet_url` (makes title a clickable link to X)\n- `description`: 2–4 sentences of plain-text intelligence analysis for this specific event type, drawing only on its prefix fields\n- `fields`: include only fields with real values (not \"unknown\", \"null\", or empty arrays); short values set `inline: true`; all values must be strings; use human-readable words instead of true|false\n- Omit `fields` array entirely if no meaningful extracted data exists\n\n### Output format\n\nRespond ONLY with a JSON object with one key per detected event. Key names follow the pattern `{prefix}_embed`.\n\nExample output when kinetic and casualty were detected:\n{\"kinetic_embed\": {\"color\": 10038562, \"title\": \"💥 Kinetic Event\", \"url\": \"https://x.com/...\", \"description\": \"...\", \"fields\": [{\"name\": \"Attacker\", \"value\": \"IRGC\", \"inline\": true}]}, \"casualty_embed\": {\"color\": 10038562, \"title\": \"💀 Casualty Report\", \"url\": \"https://x.com/...\", \"description\": \"...\", \"fields\": []}}"
  }
}
```

---

## Step 2 — Create 20 new notification actions

Create each with `POST /api/actions`, then activate with `POST /api/actions/{id}/activate`.

Record each returned ID — you will need them for the pipeline graph in Step 3.

### 2a. Thread Creator
```json
{
  "name": "Discord Thread Creator",
  "type": "notification",
  "config": {
    "channel_id": 9,
    "message_template": "@${tweet_author}: ${tweet_short}"
  }
}
```
→ Save ID as `{{THREAD_CREATOR_ID}}`

### 2b. Tweet Content
```json
{
  "name": "Discord Tweet Content",
  "type": "notification",
  "config": {
    "channel_id": 9,
    "message_template": "**[@${tweet_author}](https://x.com/${tweet_author})**\n\n${tweet_short}\n\n[🐦 Open on X](${tweet_url}) • [👁️ View in Watcher](https://watcher.the-h.me/tweets?tweet=${tweet_id})"
  }
}
```
→ Save ID as `{{TWEET_CONTENT_ID}}`

### 2c–2t. Per-event senders (18 actions)

Create each with the config shown. The `message_template` is just `${key}` — `extractEmbedBody` parses the JSON output from Action 84 and sends it as a Discord embed.

| # | Name | message_template | Save ID as |
|---|------|-----------------|------------|
| 2c | Discord Send: Assassination | `${assassination_embed}` | `{{SEND_ASSASSINATION_ID}}` |
| 2d | Discord Send: Kinetic Event | `${kinetic_embed}` | `{{SEND_KINETIC_ID}}` |
| 2e | Discord Send: Missile/Drone Strike | `${strike_embed}` | `{{SEND_STRIKE_ID}}` |
| 2f | Discord Send: Nuclear Activity | `${nuclear_embed}` | `{{SEND_NUCLEAR_ID}}` |
| 2g | Discord Send: Proxy Force | `${proxy_embed}` | `{{SEND_PROXY_ID}}` |
| 2h | Discord Send: Escalation Rhetoric | `${rhetoric_embed}` | `{{SEND_RHETORIC_ID}}` |
| 2i | Discord Send: Cyber Attack | `${cyber_embed}` | `{{SEND_CYBER_ID}}` |
| 2j | Discord Send: Diplomatic/Ceasefire | `${diplomatic_embed}` | `{{SEND_DIPLOMATIC_ID}}` |
| 2k | Discord Send: Sanctions | `${sanctions_embed}` | `{{SEND_SANCTIONS_ID}}` |
| 2l | Discord Send: Military Mobilization | `${mobilization_embed}` | `{{SEND_MOBILIZATION_ID}}` |
| 2m | Discord Send: Hostage/Prisoner | `${hostage_embed}` | `{{SEND_HOSTAGE_ID}}` |
| 2n | Discord Send: Casualty Report | `${casualty_embed}` | `{{SEND_CASUALTY_ID}}` |
| 2o | Discord Send: Evacuation | `${evacuation_embed}` | `{{SEND_EVACUATION_ID}}` |
| 2p | Discord Send: Protest/Civil Unrest | `${protest_embed}` | `{{SEND_PROTEST_ID}}` |
| 2q | Discord Send: Maritime Incident | `${maritime_embed}` | `{{SEND_MARITIME_ID}}` |
| 2r | Discord Send: Leadership Event | `${leadership_embed}` | `{{SEND_LEADERSHIP_ID}}` |
| 2s | Discord Send: Air Defense | `${airdefense_embed}` | `{{SEND_AIRDEFENSE_ID}}` |
| 2t | Discord Send: Energy Infrastructure | `${energy_embed}` | `{{SEND_ENERGY_ID}}` |

All 20 actions use `channel_id: 9`. Each config:
```json
{
  "name": "<name from table>",
  "type": "notification",
  "config": {
    "channel_id": 9,
    "message_template": "<template from table>"
  }
}
```

---

## Step 3 — Create Pipeline 30 and save its graph

### 3a. Create the pipeline
`POST /api/pipelines`
```json
{
  "name": "Discord War Notification Thread",
  "description": "Creates a Discord Forum thread per tweet and sends per-event embeds as thread replies"
}
```
→ Save returned ID as `{{PIPELINE_ID}}` (expected: 30)

### 3b. Activate it
`POST /api/pipelines/{{PIPELINE_ID}}/activate`

### 3c. Save the graph
`PUT /api/pipelines/{{PIPELINE_ID}}/graph`

Replace all `{{...}}` placeholders with the action IDs recorded in Step 2 before sending.

```json
{
  "nodes": [
    {"id": 1, "type": "start", "label": "Start", "pos_x": 80, "pos_y": 300},
    {"id": 2, "type": "action", "action_id": "{{THREAD_CREATOR_ID}}", "label": "Thread Creator", "pos_x": 280, "pos_y": 300},
    {"id": 3, "type": "action", "action_id": "{{TWEET_CONTENT_ID}}", "label": "Tweet Content", "pos_x": 480, "pos_y": 300},
    {"id": 4, "type": "action", "action_id": 84, "label": "War Analytics Generator", "pos_x": 680, "pos_y": 300},

    {"id": 5,  "type": "condition", "label": "Assassination?",  "conditions": [{"key": "assassination_embed",  "op": "exists"}], "pos_x": 900, "pos_y": -820},
    {"id": 6,  "type": "action",    "action_id": "{{SEND_ASSASSINATION_ID}}",  "label": "Send: Assassination",  "pos_x": 1100, "pos_y": -820},
    {"id": 7,  "type": "condition", "label": "Kinetic?",        "conditions": [{"key": "kinetic_embed",        "op": "exists"}], "pos_x": 900, "pos_y": -640},
    {"id": 8,  "type": "action",    "action_id": "{{SEND_KINETIC_ID}}",        "label": "Send: Kinetic",        "pos_x": 1100, "pos_y": -640},
    {"id": 9,  "type": "condition", "label": "Strike?",         "conditions": [{"key": "strike_embed",         "op": "exists"}], "pos_x": 900, "pos_y": -460},
    {"id": 10, "type": "action",    "action_id": "{{SEND_STRIKE_ID}}",         "label": "Send: Strike",         "pos_x": 1100, "pos_y": -460},
    {"id": 11, "type": "condition", "label": "Nuclear?",        "conditions": [{"key": "nuclear_embed",        "op": "exists"}], "pos_x": 900, "pos_y": -280},
    {"id": 12, "type": "action",    "action_id": "{{SEND_NUCLEAR_ID}}",        "label": "Send: Nuclear",        "pos_x": 1100, "pos_y": -280},
    {"id": 13, "type": "condition", "label": "Proxy?",          "conditions": [{"key": "proxy_embed",          "op": "exists"}], "pos_x": 900, "pos_y": -100},
    {"id": 14, "type": "action",    "action_id": "{{SEND_PROXY_ID}}",          "label": "Send: Proxy",          "pos_x": 1100, "pos_y": -100},
    {"id": 15, "type": "condition", "label": "Rhetoric?",       "conditions": [{"key": "rhetoric_embed",       "op": "exists"}], "pos_x": 900, "pos_y": 80},
    {"id": 16, "type": "action",    "action_id": "{{SEND_RHETORIC_ID}}",       "label": "Send: Rhetoric",       "pos_x": 1100, "pos_y": 80},
    {"id": 17, "type": "condition", "label": "Cyber?",          "conditions": [{"key": "cyber_embed",          "op": "exists"}], "pos_x": 900, "pos_y": 260},
    {"id": 18, "type": "action",    "action_id": "{{SEND_CYBER_ID}}",          "label": "Send: Cyber",          "pos_x": 1100, "pos_y": 260},
    {"id": 19, "type": "condition", "label": "Diplomatic?",     "conditions": [{"key": "diplomatic_embed",     "op": "exists"}], "pos_x": 900, "pos_y": 440},
    {"id": 20, "type": "action",    "action_id": "{{SEND_DIPLOMATIC_ID}}",     "label": "Send: Diplomatic",     "pos_x": 1100, "pos_y": 440},
    {"id": 21, "type": "condition", "label": "Sanctions?",      "conditions": [{"key": "sanctions_embed",      "op": "exists"}], "pos_x": 900, "pos_y": 620},
    {"id": 22, "type": "action",    "action_id": "{{SEND_SANCTIONS_ID}}",      "label": "Send: Sanctions",      "pos_x": 1100, "pos_y": 620},
    {"id": 23, "type": "condition", "label": "Mobilization?",   "conditions": [{"key": "mobilization_embed",   "op": "exists"}], "pos_x": 900, "pos_y": 800},
    {"id": 24, "type": "action",    "action_id": "{{SEND_MOBILIZATION_ID}}",   "label": "Send: Mobilization",   "pos_x": 1100, "pos_y": 800},
    {"id": 25, "type": "condition", "label": "Hostage?",        "conditions": [{"key": "hostage_embed",        "op": "exists"}], "pos_x": 900, "pos_y": 980},
    {"id": 26, "type": "action",    "action_id": "{{SEND_HOSTAGE_ID}}",        "label": "Send: Hostage",        "pos_x": 1100, "pos_y": 980},
    {"id": 27, "type": "condition", "label": "Casualty?",       "conditions": [{"key": "casualty_embed",       "op": "exists"}], "pos_x": 900, "pos_y": 1160},
    {"id": 28, "type": "action",    "action_id": "{{SEND_CASUALTY_ID}}",       "label": "Send: Casualty",       "pos_x": 1100, "pos_y": 1160},
    {"id": 29, "type": "condition", "label": "Evacuation?",     "conditions": [{"key": "evacuation_embed",     "op": "exists"}], "pos_x": 900, "pos_y": 1340},
    {"id": 30, "type": "action",    "action_id": "{{SEND_EVACUATION_ID}}",     "label": "Send: Evacuation",     "pos_x": 1100, "pos_y": 1340},
    {"id": 31, "type": "condition", "label": "Protest?",        "conditions": [{"key": "protest_embed",        "op": "exists"}], "pos_x": 900, "pos_y": 1520},
    {"id": 32, "type": "action",    "action_id": "{{SEND_PROTEST_ID}}",        "label": "Send: Protest",        "pos_x": 1100, "pos_y": 1520},
    {"id": 33, "type": "condition", "label": "Maritime?",       "conditions": [{"key": "maritime_embed",       "op": "exists"}], "pos_x": 900, "pos_y": 1700},
    {"id": 34, "type": "action",    "action_id": "{{SEND_MARITIME_ID}}",       "label": "Send: Maritime",       "pos_x": 1100, "pos_y": 1700},
    {"id": 35, "type": "condition", "label": "Leadership?",     "conditions": [{"key": "leadership_embed",     "op": "exists"}], "pos_x": 900, "pos_y": 1880},
    {"id": 36, "type": "action",    "action_id": "{{SEND_LEADERSHIP_ID}}",     "label": "Send: Leadership",     "pos_x": 1100, "pos_y": 1880},
    {"id": 37, "type": "condition", "label": "Air Defense?",    "conditions": [{"key": "airdefense_embed",     "op": "exists"}], "pos_x": 900, "pos_y": 2060},
    {"id": 38, "type": "action",    "action_id": "{{SEND_AIRDEFENSE_ID}}",     "label": "Send: Air Defense",    "pos_x": 1100, "pos_y": 2060},
    {"id": 39, "type": "condition", "label": "Energy?",         "conditions": [{"key": "energy_embed",         "op": "exists"}], "pos_x": 900, "pos_y": 2240},
    {"id": 40, "type": "action",    "action_id": "{{SEND_ENERGY_ID}}",         "label": "Send: Energy",         "pos_x": 1100, "pos_y": 2240}
  ],
  "edges": [
    {"source_node_id": 1,  "source_handle": "default", "target_node_id": 2},
    {"source_node_id": 2,  "source_handle": "default", "target_node_id": 3},
    {"source_node_id": 3,  "source_handle": "default", "target_node_id": 4},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 5},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 7},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 9},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 11},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 13},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 15},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 17},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 19},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 21},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 23},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 25},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 27},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 29},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 31},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 33},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 35},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 37},
    {"source_node_id": 4,  "source_handle": "default", "target_node_id": 39},
    {"source_node_id": 5,  "source_handle": "pass", "target_node_id": 6},
    {"source_node_id": 7,  "source_handle": "pass", "target_node_id": 8},
    {"source_node_id": 9,  "source_handle": "pass", "target_node_id": 10},
    {"source_node_id": 11, "source_handle": "pass", "target_node_id": 12},
    {"source_node_id": 13, "source_handle": "pass", "target_node_id": 14},
    {"source_node_id": 15, "source_handle": "pass", "target_node_id": 16},
    {"source_node_id": 17, "source_handle": "pass", "target_node_id": 18},
    {"source_node_id": 19, "source_handle": "pass", "target_node_id": 20},
    {"source_node_id": 21, "source_handle": "pass", "target_node_id": 22},
    {"source_node_id": 23, "source_handle": "pass", "target_node_id": 24},
    {"source_node_id": 25, "source_handle": "pass", "target_node_id": 26},
    {"source_node_id": 27, "source_handle": "pass", "target_node_id": 28},
    {"source_node_id": 29, "source_handle": "pass", "target_node_id": 30},
    {"source_node_id": 31, "source_handle": "pass", "target_node_id": 32},
    {"source_node_id": 33, "source_handle": "pass", "target_node_id": 34},
    {"source_node_id": 35, "source_handle": "pass", "target_node_id": 36},
    {"source_node_id": 37, "source_handle": "pass", "target_node_id": 38},
    {"source_node_id": 39, "source_handle": "pass", "target_node_id": 40}
  ]
}
```

---

## Step 4 — Create the pipeline_finished trigger link

`POST /api/triggers/links`
```json
{
  "trigger_type": "pipeline_finished",
  "pipeline_id": "{{PIPELINE_ID}}",
  "source_pipeline_id": 22
}
```
→ Save ID as `{{TRIGGER_LINK_ID}}`

`POST /api/triggers/links/{{TRIGGER_LINK_ID}}/activate`

---

## Step 5 — Deactivate old Pipeline 23

The old "Iran Conflict Notification" pipeline (id: 23) overlaps with this new flow. Deactivate it so it no longer fires:

`POST /api/pipelines/23/deactivate`

You can also deactivate Action 85 ("War Analytics Discord Notification") if you want to prevent any residual single-embed sends:

`POST /api/actions/85/deactivate`

---

## Execution order summary

```
1. PUT  /api/actions/84                          # update generator system prompt + context_action_ids
2. POST /api/actions  (×20)                      # create 20 notification actions, record all IDs
3. POST /api/actions/{id}/activate  (×20)        # activate all 20
4. POST /api/pipelines                           # create Pipeline 30
5. POST /api/pipelines/{id}/activate
6. PUT  /api/pipelines/{id}/graph               # paste graph JSON with real action IDs substituted
7. POST /api/triggers/links                     # pipeline_finished link for Pipeline 22
8. POST /api/triggers/links/{id}/activate
9. POST /api/pipelines/23/deactivate            # retire old notification pipeline
10. POST /api/actions/85/deactivate             # retire old single-embed send action
```

---

## How it works at runtime

```
New tweet arrives
  → Pipeline 22 ("Iran Conflict Analytics") fires
    → condition: is it war-related?
      → YES: 19 sub-pipelines run in parallel (detectors + extractors)
      → NO: Like and Retweet pipeline

  → All Pipeline 22 tasks complete
    → pipeline_finished trigger fires
      → Pipeline 30 ("Discord War Notification Thread") starts:

        [Thread Creator]
          POST webhook?wait=true
          thread_name = "@author: tweet preview..."
          → saves discord_thread_id to metadata

        [Tweet Content]
          POST webhook?thread_id={discord_thread_id}
          sends author link + tweet text + X link + watcher link

        [War Analytics Generator] (Action 84)
          reads all sub-pipeline metadata from context
          outputs: kinetic_embed, casualty_embed, ... (only detected events)
          → saves per-event embed keys to metadata

        [18 parallel condition+send branches]
          each checks if its embed key exists
          if yes: POST webhook?thread_id={discord_thread_id}
          sends that embed as a Discord embed in the thread
```
