#!/usr/bin/env bash
# prepare-nominatim.sh — Download country PBF extracts and merge them into a
# single file for the self-hosted Nominatim instance.
#
# Usage:
#   ./scripts/prepare-nominatim.sh          # download + merge
#   ./scripts/prepare-nominatim.sh download # download only
#   ./scripts/prepare-nominatim.sh merge    # merge only (PBFs already present)
#
# PBF files are saved to nominatim-data/ (bind-mounted into the Nominatim container).
# The merge step runs in a Docker container (ubuntu:24.04 + osmium-tool).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DATA_DIR="$SCRIPT_DIR/../nominatim-pbf"
OUTPUT="$DATA_DIR/middle-east.osm.pbf"

COUNTRIES=(
  "https://download.geofabrik.de/asia/iran-latest.osm.pbf"
  "https://download.geofabrik.de/asia/iraq-latest.osm.pbf"
  "https://download.geofabrik.de/asia/israel-and-palestine-latest.osm.pbf"
  "https://download.geofabrik.de/asia/lebanon-latest.osm.pbf"
  "https://download.geofabrik.de/asia/jordan-latest.osm.pbf"
  "https://download.geofabrik.de/asia/azerbaijan-latest.osm.pbf"
  "https://download.geofabrik.de/asia/pakistan-latest.osm.pbf"
)

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
      rm -f middle-east.osm.pbf
      echo 'Merging...'
      osmium merge $FILES -o middle-east.osm.pbf
      echo 'Cleaning up individual PBFs...'
      rm -f $FILES
      echo 'Done. middle-east.osm.pbf is ready.'
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
