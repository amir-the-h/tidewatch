#!/bin/sh
set -e

# Write runtime config so the SPA can read API_BASE_URL without a build-time bake-in.
# Leave API_BASE_URL empty (default) so the SPA uses relative paths — API calls are
# then proxied internally by nginx to API_UPSTREAM without leaving the server.
cat > /usr/share/nginx/html/env-config.js <<EOF
window.__ENV__ = { "API_BASE_URL": "${API_BASE_URL:-}" };
EOF

# Substitute ${API_UPSTREAM} in the nginx config template.
# Only substitutes API_UPSTREAM; nginx variables like $host are left untouched.
API_UPSTREAM="${API_UPSTREAM:-http://iran-watcher:1452}" \
  envsubst '${API_UPSTREAM}' \
  < /etc/nginx/conf.d/default.conf.template \
  > /etc/nginx/conf.d/default.conf

exec nginx -g 'daemon off;'
