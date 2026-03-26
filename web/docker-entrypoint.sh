#!/bin/sh
set -e

# Write runtime config so the SPA can read API_BASE_URL without a build-time bake-in.
# Set API_BASE_URL env var when running the container; defaults to empty string (same-origin).
cat > /usr/share/nginx/html/env-config.js <<EOF
window.__ENV__ = { "API_BASE_URL": "${API_BASE_URL:-}" };
EOF

exec nginx -g 'daemon off;'
