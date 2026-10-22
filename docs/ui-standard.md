# Tidewatch Console — UI standard

Tidewatch Console is the interface standard for the Tidewatch web UI: a dense, dark-first operations console for watching a live tweet stream, building processing pipelines and reviewing what they produce. It is implemented on **Naive UI today** and structured so the app can move to **shadcn-vue later** by swapping component internals, not rewriting pages.

Borrowed foundations: IBM Carbon's rules for dense operational UIs (4px grid, productive type scale, tables for collections), and shadcn-vue's semantic colour names (`background`, `primary`, `muted-foreground`…) so the CSS variables never need renaming.

## Principles

- **Density over decoration.** Operators scan, compare and act. Default to `small` controls (`control-sm`, 28px), `body` (13px) text and tables. Whitespace separates groups; it is not filler.
- **State is visible at a glance.** Anything live (stream, queue, workers, backends) has a status indicator: a dot in `success`/`warning`/`destructive`/`info` plus a word. Never colour alone.
- **One accent.** `primary` (tide teal) marks the one main action, the active nav item, links and focus. Everything else is neutral.
- **Data looks like data.** IDs, keys, JSON, prompts and counters are set in `mono`/`metric` with tabular figures.
- **Every view has four states:** loading (skeleton in `muted`), empty (what will appear here and how to add the first item), error (what failed and how to fix it), and populated.

## Architecture: built for a later move to shadcn-vue

The overhaul is organised in four layers. Pages only touch the top two.

| Layer | Lives in | Today (Naive UI) | Later (shadcn-vue) |
|---|---|---|---|
| **Tokens** | `web/src/theme/tokens.ts` → `tokens.css` | CSS variables named as shadcn expects, plus a `naiveOverrides` object derived from the same values | shadcn reads the same `--background`, `--primary`… unchanged |
| **Layout** | Tailwind v4 utilities | `flex gap-2 px-6` etc., theme mapped to the tokens via `@theme inline` | Identical — shadcn-vue is built on Tailwind |
| **UI kit** | `web/src/components/ui/<Component>/` | Thin wrappers over `n-*` components exposing **shadcn-vue's names and props** (`<Button variant="destructive" size="sm">`) | Replace each wrapper's internals with the shadcn-vue component; pages unchanged |
| **Feedback** | `web/src/composables/useToast.ts`, `useConfirm.ts` | Wraps Naive `useMessage` / `useDialog` | Wraps vue-sonner / AlertDialog |

Rules that keep the move cheap:

- **Pages and feature components never use `n-*` tags directly.** They import from `@/components/ui`. Naive is an implementation detail of that folder.
- **No `n-space`, `n-flex`, `n-grid` for layout.** Use Tailwind utilities (`flex`, `grid`, `gap-*`). These are the most-used Naive tags today and the hardest to migrate later.
- **No hex values and no inline `style=""` for colour, spacing or type** in `.vue` files. Use tokens through Tailwind classes (`bg-card`, `text-muted-foreground`, `gap-3`). A CI grep enforces this.
- **Wrapper props follow shadcn-vue**, even where Naive names differ: `variant` not `type`, `size: 'sm' | 'default' | 'lg' | 'icon'`, `v-model:open` for overlays.
- **Icons are `@lucide/vue`**, shadcn-vue's icon set, replacing `@vicons/ionicons5`. 16px in controls, 14px inline with `small` text, stroke width 1.75.
- **Components with no shadcn equivalent** (vue-flow pipeline canvas, Leaflet map, vis-network graph) are styled from tokens directly and stay as they are.

Mapping of Naive pieces to their wrapper and future replacement:

| Wrapper (`@/components/ui`) | Wraps today | shadcn-vue later |
|---|---|---|
| `Button` | `n-button` | Button |
| `Badge`, `StatusIndicator` | `n-tag`, custom | Badge |
| `Input`, `Textarea`, `NumberField` | `n-input`, `n-input-number` | Input, Textarea, NumberField |
| `Select`, `Combobox` | `n-select` | Select, Combobox |
| `Switch`, `Checkbox`, `RadioGroup` | `n-switch`, `n-checkbox`, `n-radio` | same names |
| `FormField` | `n-form-item` | FormField (vee-validate) |
| `DataTable` | `n-data-table` | Table + TanStack Table |
| `Sheet` | `n-drawer` (FormDrawer) | Sheet |
| `Dialog`, `AlertDialog` | `n-modal`, `n-popconfirm` | Dialog, AlertDialog |
| `Tabs`, `Tooltip`, `Card`, `Alert`, `Skeleton` | `n-tabs`, `n-tooltip`, `n-card`, `n-alert`, `n-spin` | same names |
| `DatePicker` | `n-date-picker` | Calendar + Popover |

## Colour

- Page canvas is `background`; tables, cards, drawers and the status bar sit on `card` with a `border` hairline. Menus and tooltips use `popover`.
- Body text is `foreground`. Descriptions, timestamps and help text are `muted-foreground` — never lower contrast than that.
- `primary` is the only brand colour. Text on it is `primary-foreground` (dark ink in the dark theme, white in light).
- `accent` is shadcn's name for the **neutral hover fill** (ghost buttons, menu items, table row hover). It is not a brand colour.
- Status colours mean exactly one thing each: `success` running/healthy/connected, `warning` degraded/backing up/needs review, `destructive` failed/disconnected/delete, `info` in progress. Pair each with a word.
- Charts and entity types use `chart-1`…`chart-5` in order. Entity types are fixed: person `chart-4`, organisation `chart-3`, location `chart-5`.
- The map's date-heat pins use `heat-0` (oldest) to `heat-4` (newest). They sit on map tiles only.
- There is one theme, **Tidewatch Dark**. No theme switching or light mode is planned.

## Type

- Set the whole UI in IBM Plex Sans (`--font-sans`), loaded from Google Fonts. Use IBM Plex Mono (`--font-mono`) for tweet IDs, action keys, JSON, Ollama prompts and live counters.
- Base text is `body` (13px/20px). Help text and secondary cells are `small` (12px). Nothing below `label` (11px).
- One `page-title` per page. Drawers and dialogs use `title`. Uppercase only for `label` (sidebar groups, table headers), with its 0.04em tracking.
- All numbers that update or line up in columns use `font-variant-numeric: tabular-nums`.

## Spacing, size and shape

- 4px grid: `space-1` 4, `space-2` 8, `space-3` 12, `space-4` 16, `space-6` 24, `space-8` 32. No other values.
- Page gutter `space-6` on desktop, `space-4` under 640px. Sections inside a page are `space-6` apart; fields in a form `space-3`; controls in a toolbar `space-2`.
- Controls are `control-sm` (28px) by default. `control-md` (32px) only on the login form and drawer footers.
- Radius: `radius-sm` badges and tags, `radius-md` buttons and inputs, `radius-lg` cards, drawers, dialogs. No pills except status dots.
- Elevation is borders, not shadows. Only popovers and dialogs get a shadow.

## Layout

The **AppShell** replaces today's hamburger drawer:

- A **left sidebar** (`sidebar-w` 224px, collapsible to `sidebar-w-collapsed` 52px; an off-canvas Sheet under 768px) with three groups:
  - **Monitor:** Map, Tweets, Tracker, Digest, Chat
  - **Build:** Sources, Actions, Pipelines, Notifications
  - **System:** Queue, Users
  - Viewers see only Map, Tweets and Tracker, ungrouped.
- A **status bar** (`statusbar-h` 40px) across the content area: stream state with connect/disconnect, queue depth with ETA, failed count, worker utilisation, then the user menu on the right.
- Every page follows **PageHeader → toolbar → content**. PageHeader holds the `page-title`, a one-line description in `muted-foreground`, and at most one primary action. The toolbar holds search and filters. Content is usually a DataTable.
- Create and edit happen in a right-side **Sheet** (480px, full width on mobile), not a page or modal. Destructive confirmations use **AlertDialog**.
- Full-bleed views (Map, Pipeline builder, Connections graph) drop the page gutter but keep the PageHeader.

## Interaction

- Focus: a solid 2px `ring` outline with 2px offset on every focusable element. Never remove it.
- Destructive actions (delete, flush queue, stop workers, disconnect stream) always confirm, and the confirm button is the `destructive` variant naming the action ("Flush 214 tasks"), not "OK".
- Toasts confirm completed actions in past tense ("Pipeline activated"). Errors say what failed and what to do.
- Live values update in place without layout shift; don't animate numbers.
- Respect `prefers-reduced-motion`: no slide-in for Sheets, only fades.

## Writing

- Sentence case everywhere: "New pipeline", not "New Pipeline".
- Buttons say what happens: "Activate", "Run backfill", "Delete 3 rules". Avoid "Submit", "OK".
- Use the product's own nouns consistently: tweet, source, rule, action, pipeline, trigger, notification channel, tracked entity, candidate, digest.
- Empty states name what will appear and the first step: "No pipelines yet. Create one to start processing tweets from a trigger."
- No emoji in UI copy.

## Iconography

- `@lucide/vue`, outline style, stroke 1.75. 16px in buttons and nav, 14px inline with `small` text.
- Icon-only buttons always have a tooltip and an `aria-label`.
- No logo exists yet; the wordmark is the name "Tidewatch" set in `title` weight 600.

## Accessibility

- Text pairs meet WCAG AA (4.5:1) in both themes; `muted-foreground` is checked against `background`, `card` and `muted`.
- Control borders (`input`) and the focus ring meet 3:1 against `card`.
- Sidebar, tables, tabs and menus are fully keyboard-operable; tables support arrow-key row focus when rows are clickable.

## Enforcement

- `web/scripts/check-ui.sh` (run in CI next to `vue-tsc`): fails on `#[0-9a-f]{3,8}` or `style="` in `src/**/*.vue`, and on `<n-` outside `src/components/ui/`.
- During the migration each page is converted whole; the check runs per converted directory until all pages are done.

## Implementation notes

- **Tokens** live in `web/src/styles/main.css` as CSS variables on `:root`, mapped into Tailwind with `@theme inline` (`bg-card`, `text-muted-foreground`, `border-border`, `h-statusbar`, `w-sidebar`…). Type sizes are Tailwind text utilities: `text-label`, `text-small`, `text-body`, `text-title`, `text-page-title`, `text-display`.
- **Naive UI** gets its theme from `web/src/theme/naive.ts`, which reads those variables at startup (`buildNaiveOverrides()`), so colours are never duplicated in TypeScript.
- **Tailwind classes on Naive components only set layout** (margin, width, flex). Naive's styles are unlayered and beat Tailwind's layered utilities, so visual properties (colour, padding, radius) come from the theme overrides, not classes.
- **UI kit:** `web/src/components/ui/` (`AlertDialog`, `Badge`, `Button`, `Checkbox`, `Combobox` (searchable multi-select), `DataTable` (+ `DataTableColumns` type; `page-size` for client-side paging), `DatePicker`, `Dialog`, `FormField`, `IconButton`, `Input`, `RadioGroup`, `Select`, `Sheet`, `Skeleton`, `StatusIndicator`, `Switch`, `Tabs`, `Textarea`, `Tooltip`). Toasts go through `useToast()` in `web/src/composables/useToast.ts`. **Shell:** `web/src/components/layout/` (`AppShell`, `AppSidebar`, `StatusBar`, `nav.ts`). Routes with `meta.fullBleed` render without the page gutter.
- **Fonts** are self-hosted via `@fontsource/ibm-plex-sans` and `@fontsource/ibm-plex-mono` (no Google Fonts requests from deployments).
- **Check:** `npm run check:ui` (`web/scripts/check-ui.sh`) runs in CI. Add a path to its `CONVERTED` list when a page is migrated.

## Token values (Tidewatch Dark)

| Token | Value | Token | Value |
|---|---|---|---|
| `background` | `#0e1217` | `primary` | `#2fb5a8` |
| `card` | `#151a21` | `primary-foreground` | `#04201d` |
| `popover` | `#1a2029` | `destructive` | `#f0616d` |
| `muted` / `accent` | `#1f2630` | `success` | `#3fbf7f` |
| `secondary` | `#222a35` | `warning` | `#e8b04a` |
| `foreground` | `#e6ebf1` | `info` | `#5aa9e6` |
| `muted-foreground` | `#93a0b0` | `border` | `#2a323d` |
| `sidebar` | `#11161c` | `input` | `#5f6b7a` |
| `sidebar-accent` | `#1c232c` | `chart-1..5` | `#2fb5a8` `#e8b04a` `#8f8ff0` `#e9789f` `#7cc3e8` |
| `sidebar-foreground` | `#b4bfcc` | `heat-0..4` | `#3b6fd8` `#25adc4` `#4cb86b` `#e3c13b` `#e0533f` |
