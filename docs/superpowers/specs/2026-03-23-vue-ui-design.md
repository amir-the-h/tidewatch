# Vue Admin UI — Design Spec

**Date:** 2026-03-23
**Status:** Approved

## Overview

A Vue 3 single-page admin interface that exposes all REST API capabilities of the iran-watcher backend. Runs as a separate Docker container served by nginx, with the user's existing nginx reverse proxy handling external access.

## Tech Stack

| Concern | Choice |
|---|---|
| Framework | Vue 3 (Composition API, `<script setup>`) |
| Build tool | Vite |
| Language | TypeScript |
| Component library | Naive UI (dark theme) |
| State management | Pinia (one store per API resource) |
| Routing | Vue Router 4 |
| API config | `VITE_API_BASE_URL` build-time env var |

## Project Structure

Lives at `web/` in the project root.

```
web/
├── src/
│   ├── api/
│   │   ├── client.ts          # base fetch wrapper using VITE_API_BASE_URL
│   │   ├── health.ts
│   │   ├── sources.ts
│   │   ├── rules.ts
│   │   ├── tweets.ts
│   │   ├── models.ts
│   │   ├── stream.ts
│   │   ├── categorizations.ts
│   │   └── notifications.ts
│   ├── stores/
│   │   ├── sources.ts
│   │   ├── rules.ts
│   │   ├── tweets.ts
│   │   ├── models.ts
│   │   ├── stream.ts
│   │   └── notifications.ts
│   ├── views/
│   │   ├── StreamView.vue
│   │   ├── TweetsView.vue
│   │   ├── RulesView.vue
│   │   ├── SourcesView.vue
│   │   ├── ModelsView.vue
│   │   ├── CategorizationsView.vue
│   │   └── NotificationsView.vue
│   ├── components/
│   │   └── RuleExprBuilder.vue   # recursive expression tree builder
│   ├── router/
│   │   └── index.ts
│   ├── App.vue                   # sidebar layout shell
│   └── main.ts
├── Dockerfile
├── nginx.conf
├── vite.config.ts
├── tsconfig.json
└── package.json
```

## Layout

Persistent left sidebar with section links. Active section highlighted. Sidebar shows the app name ("Iran Watcher") at the top. Main content area to the right fills the viewport.

Sidebar order:
1. Stream
2. Tweets
3. Rules
4. Sources
5. Models
6. Categorizations
7. Notifications

## API Client

`src/api/client.ts` exports a single `apiFetch(path, options)` function that prepends `import.meta.env.VITE_API_BASE_URL` to every request and throws a typed `ApiError` on non-2xx responses. All resource modules (`sources.ts`, `rules.ts`, etc.) use this function — they do not call `fetch` directly.

## Pinia Stores

One store per resource. Each store holds:
- the list/item state
- loading and error flags
- actions that call the API module and update state

Components read from stores and call store actions; they do not call the API directly.

## Views

### Stream (`/`)
- Health indicator (calls `GET /api/health` on mount)
- Stream status badge — "CONNECTED" or "DISCONNECTED" (calls `GET /api/stream/status`)
- Connect button → `POST /api/stream/connect`
- Disconnect button → `POST /api/stream/disconnect`

### Tweets (`/tweets`)
- Paginated data table (page, per_page)
- Filter bar: rule selector (dropdown populated from rules store), date-from / date-to pickers
- Clicking a row opens a detail drawer showing full tweet fields plus its categorizations

### Rules (`/rules`)
- Table: name, compiled preview, active badge, created date
- Create button → opens a drawer with:
  - Name field
  - Visual expression tree builder (see below)
  - Compiled preview (read-only, updated as tree changes)
- Row actions: Edit (same drawer), Activate, Deactivate, Delete
- Delete blocked with explanation if rule has tweet history (API returns 409)

### Sources (`/sources`)
- Table: name, type, operator, value, active badge
- Create drawer: name field, type dropdown (all SourceType constants), value field. Operator is read-only, auto-populated from the selected type.
- Row actions: Deactivate, Delete

### Models (`/models`)
- Table: name, version, path, type, active badge
- Two create paths:
  - "Download from HuggingFace" — modal with `repo_id` input, calls `POST /api/models/download`
  - "Register manually" — drawer with name, version, path, type fields
- Row actions: Activate, Deactivate, Delete

### Categorizations (`/categorizations`)
- Table: tweet ID (links to tweet detail), model name, result, score, created date
- Filter: tweet ID input, model name input

### Notifications (`/notifications`)
- Table: name, event type, active badge
- Create/Edit drawer with all Notification fields:
  - name, event_type (dropdown of valid types), filter_rule_id, filter_model_name, filter_model_result
  - Pushover section: priority (dropdown 0-2), sound, device, title, URL, URL title, TTL, HTML toggle, retry, expire
  - message_template text area
  - Priority=2 (Emergency) validates retry ≥ 30 and expire 1–10800 client-side before submit
- Row actions: Test (calls `POST /api/notifications/test`), Activate, Deactivate, Delete

## Rule Expression Tree Builder

The `ExprNode` type from the API:

```typescript
type ExprNode =
  | { src: number }        // source reference leaf — ID of a Source
  | { op: string }         // raw X operator literal leaf (e.g. "is:retweet")
  | { AND: ExprNode[] }    // AND group — 1+ children
  | { OR: ExprNode[] }     // OR group — 1+ children
  | { NOT: ExprNode }      // NOT wrapper — exactly 1 child
```

`RuleExprBuilder.vue` is a recursive component. Each node renders:
- A type selector dropdown (AND / OR / NOT / Source / Op)
- For AND/OR: a list of child `RuleExprBuilder` instances plus an "Add child" button
- For NOT: a single child `RuleExprBuilder` instance
- For `src`: a dropdown populated from the sources store (shows name + operator + value)
- For `op`: a free-text input

Nodes can be removed with a delete button (except the root). The tree emits an `update:modelValue` event with the updated `ExprNode` so the parent view can track it and display the compiled preview.

A compiled preview (the raw `compiled_value` string returned from the API after creation) is shown as a read-only field beneath the tree in the rules drawer.

## Docker Deployment

### `web/Dockerfile`

Multi-stage build:
1. `node:22-alpine` — installs deps, builds with `VITE_API_BASE_URL` ARG, outputs to `/app/dist`
2. `nginx:alpine` — copies dist, uses a custom `nginx.conf` that serves SPA correctly (all paths → `index.html`)

### `web/nginx.conf`

Serves static files with `try_files $uri $uri/ /index.html` for SPA routing. Listens on port 80.

### docker-compose addition

A separate `docker-compose.yml` (or addition to the existing one at `/mnt/docker/compose/iran-watcher/`) for the UI container:

```yaml
iran-watcher-ui:
  build:
    context: /path/to/iran-watcher/web
    args:
      VITE_API_BASE_URL: "${IRAN_WATCHER_API_BASE_URL:-http://iran-watcher:1452}"
  image: iran-watcher-ui:latest
  container_name: iran-watcher-ui
  restart: unless-stopped
  ports:
    - "127.0.0.1:1453:80"
  networks:
    - shared
```

The user's existing nginx reverse proxy routes to port 1453.

## Error Handling

- All API errors surface as Naive UI notification toasts (error level)
- Loading states use Naive UI's `n-spin` or skeleton loaders on tables
- 409 Conflict on rule delete shows an explanatory message (not a generic error)

## Testing

No automated tests for the UI in scope. The app is an internal admin tool. Manual verification against the running API is sufficient.
