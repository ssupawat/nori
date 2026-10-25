#!/bin/sh
# Restore the demo app to its seeded state. Run hourly by
# .github/workflows/demo-reset.yml (FLY_API_TOKEN in the environment),
# or by hand: FLY=~/.fly/bin/fly ./demo/reset.sh
set -eu

APP=${APP:-nori-demo}
REGION=${REGION:-sin}
FLY=${FLY:-flyctl}

IMAGE=$($FLY releases -a "$APP" -j | jq -r '.[0].ImageRef')
VOLID=$($FLY volumes list -a "$APP" -j | jq -r '.[0].id')

MID=$($FLY machines list -a "$APP" -j | jq -r '.[0].id // empty')
if [ -n "$MID" ]; then
  $FLY machine stop "$MID" -a "$APP" || true
  $FLY machine destroy "$MID" -a "$APP" --force
fi

$FLY machines run "$IMAGE" -a "$APP" --region "$REGION" -v "$VOLID:/data" \
  --entrypoint /bin/sh -- -c "rm -rf /data/nori && cp -a /data/.seed/nori /data/nori && echo RESET"

# wait for the one-off to finish (it exits after echoing RESET)
i=0
while [ $i -lt 18 ]; do
  STATE=$($FLY machines list -a "$APP" -j | jq -r '.[0].state')
  [ "$STATE" = "stopped" ] && break
  sleep 5
  i=$((i + 1))
done

ONEOFF=$($FLY machines list -a "$APP" -j | jq -r '.[0].id')
$FLY machine destroy "$ONEOFF" -a "$APP" --force

$FLY deploy -a "$APP" -c demo/fly.toml --dockerfile Dockerfile.flyio \
  --build-arg NORI_VERSION=v0.2.0 --build-arg NORI_ARCH=amd64 >/dev/null
echo "demo reset"
