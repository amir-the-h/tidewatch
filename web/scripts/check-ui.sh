#!/bin/sh
# Enforces docs/ui-standard.md in directories already converted to the UI kit:
#   - no hex colours or inline style="" in .vue files (use tokens via Tailwind classes)
#   - no n-* Naive tags outside src/components/ui (import wrappers from @/components/ui)
# Add a path to CONVERTED when a page or component has been migrated.
set -eu
cd "$(dirname "$0")/.."

CONVERTED="src/App.vue src/components/layout src/components/ui src/components/PageHeader.vue src/views/PipelinesView.vue src/components/tabs/PipelinesTab.vue src/components/tabs/TriggersTab.vue src/components/tabs/ExecutionsTab.vue src/views/SourcesView.vue src/components/tabs/SourcesTab.vue src/components/tabs/RulesTab.vue src/components/RuleExprBuilder.vue"

fail=0
report() { echo "check-ui: $1"; echo "$2"; fail=1; }

# shellcheck disable=SC2086
out=$(grep -rnE --include='*.vue' '#[0-9a-fA-F]{3,8}\b' $CONVERTED || true)
[ -n "$out" ] && report "hex colour in a converted file (use a token)" "$out"

# shellcheck disable=SC2086
out=$(grep -rnE --include='*.vue' '(^|[[:space:]])(:?style)=' $CONVERTED || true)
[ -n "$out" ] && report "inline style in a converted file (use Tailwind classes)" "$out"

naive_scope=$(for p in $CONVERTED; do case "$p" in src/components/ui*) ;; *) echo "$p" ;; esac; done)
# shellcheck disable=SC2086
out=$(grep -rnE --include='*.vue' '<n-[a-z]' $naive_scope | grep -v '^src/App.vue:.*n-\(config\|message\)-provider' || true)
[ -n "$out" ] && report "Naive tag outside src/components/ui (use a wrapper)" "$out"

[ "$fail" -eq 0 ] && echo "check-ui: ok"
exit "$fail"
