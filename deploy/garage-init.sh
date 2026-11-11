#!/bin/sh
# One-shot set-up for Garage: assigns the node layout, imports the
# access key from the environment and creates the two buckets. Safe to run
# again: each step checks what already exists.
set -eu
apk add --no-cache curl jq >/dev/null

A="http://garage:3903/v2"
auth="Authorization: Bearer ${GARAGE_ADMIN_TOKEN}"
get() { curl -sf -H "$auth" "$A/$1"; }
post() { curl -sf -H "$auth" -H 'Content-Type: application/json' -X POST "$A/$1" -d "$2"; }

echo "waiting for garage"
until get GetClusterStatus >/tmp/status.json 2>/dev/null; do sleep 1; done
node=$(jq -r '.nodes[0].id' /tmp/status.json)

if [ "$(get GetClusterLayout | jq '.roles | length')" = "0" ]; then
  echo "assigning layout to $node"
  post UpdateClusterLayout "{\"roles\":[{\"id\":\"$node\",\"zone\":\"dc1\",\"capacity\":${GARAGE_CAPACITY_BYTES:-107374182400},\"tags\":[]}]}" >/dev/null
  version=$(get GetClusterLayout | jq '.version')
  post ApplyClusterLayout "{\"version\":$((version + 1))}" >/dev/null
fi

if ! get "GetKeyInfo?id=${KOWHAI_S3_ACCESS_KEY}" >/dev/null 2>&1; then
  echo "importing key"
  post ImportKey "{\"name\":\"kowhai\",\"accessKeyId\":\"${KOWHAI_S3_ACCESS_KEY}\",\"secretAccessKey\":\"${KOWHAI_S3_SECRET_KEY}\"}" >/dev/null
fi

for bucket in "${KOWHAI_S3_BUCKET}" "${KOWHAI_S3_BACKUP_BUCKET}"; do
  if ! info=$(get "GetBucketInfo?globalAlias=$bucket" 2>/dev/null); then
    echo "creating bucket $bucket"
    info=$(post CreateBucket "{\"globalAlias\":\"$bucket\"}")
  fi
  id=$(echo "$info" | jq -r '.id')
  post AllowBucketKey "{\"bucketId\":\"$id\",\"accessKeyId\":\"${KOWHAI_S3_ACCESS_KEY}\",\"permissions\":{\"read\":true,\"write\":true,\"owner\":true}}" >/dev/null
done
echo "garage ready"
