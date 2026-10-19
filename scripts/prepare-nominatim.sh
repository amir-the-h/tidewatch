#!/usr/bin/env bash
# prepare-nominatim.sh — Download Geofabrik PBF extracts and merge them into a
# single file for the self-hosted Nominatim instance.
#
# Usage:
#   ./scripts/prepare-nominatim.sh          # download + merge
#   ./scripts/prepare-nominatim.sh download # download only
#   ./scripts/prepare-nominatim.sh merge    # merge only (PBFs already present)
#
# Regions are Geofabrik paths (see https://download.geofabrik.de), space-separated:
#   GEOFABRIK_REGIONS="europe/germany europe/austria" ./scripts/prepare-nominatim.sh
# The default is a tiny extract that imports in a minute — handy for trying things out.
#
# Output name defaults to regions.osm.pbf (override with NOMINATIM_PBF_NAME; must
# match the PBF_PATH in docker-compose.yml).
#
# PBF files are saved to nominatim-pbf/ (bind-mounted into the Nominatim container).
# The merge step runs in a Docker container (ubuntu:24.04 + osmium-tool).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DATA_DIR="$SCRIPT_DIR/../nominatim-pbf"
OUTPUT_NAME="${NOMINATIM_PBF_NAME:-regions.osm.pbf}"
OUTPUT="$DATA_DIR/$OUTPUT_NAME"

read -r -a REGIONS <<< "${GEOFABRIK_REGIONS:-europe/monaco}"
COUNTRIES=()
for REGION in "${REGIONS[@]}"; do
  COUNTRIES+=("https://download.geofabrik.de/${REGION}-latest.osm.pbf")
done

download() {
  echo "==> Downloading PBF extracts to $DATA_DIR"
  mkdir -p "$DATA_DIR"
  for URL in "${COUNTRIES[@]}"; do
    FILE="$DATA_DIR/$(basename "$URL")"
    if [ -f "$FILE" ]; then
      echo "  [skip] $(basename "$FILE") already exists"
    else
      echo "  $(basename "$FILE")"
      wget -q --show-progress -O "$FILE" "$URL"
    fi
  done
  echo "==> Download complete"
}

merge() {
  echo "==> Merging PBFs with osmium (via Docker)"
  FILES=""
  for URL in "${COUNTRIES[@]}"; do
    FILES="$FILES $(basename "$URL")"
  done

  docker run --rm \
    -v "$DATA_DIR:/work" \
    ubuntu:24.04 \
    sh -c "
      set -e
      apt-get update -qq && apt-get install -y -qq osmium-tool
      cd /work
      rm -f $OUTPUT_NAME
      echo 'Merging...'
      osmium merge $FILES -o $OUTPUT_NAME
      echo 'Cleaning up individual PBFs...'
      rm -f $FILES
      echo 'Done. $OUTPUT_NAME is ready.'
    "
  echo "==> Merge complete: $OUTPUT"
}

MODE="${1:-all}"

case "$MODE" in
  download) download ;;
  merge)    merge ;;
  all)      download && merge ;;
  *)
    echo "Usage: $0 [download|merge|all]"
    exit 1
    ;;
esac
