#!/usr/bin/env bash
# CI check against a real Kanidm server. It registers Kōwhai as an OAuth2
# client, starts Kōwhai pointed at Kanidm, and checks that:
#   - Kōwhai loads Kanidm's discovery document and ES256 signing keys;
#   - /auth/login sends the browser to Kanidm with a PKCE S256 challenge;
#   - Kanidm accepts that authorisation request.
# A full password sign-in needs Kanidm's interactive credential set-up, so
# it is not automated here. The Authelia job covers the full flow.
set -euo pipefail

NET=kowhai-kanidm
IMG=${KOWHAI_IMAGE:-kowhai:ci}
KANIDM=kanidm/server:1.12.0
TOOLS=kanidm/tools:1.12.0
WORK=$(mktemp -d)
trap 'docker rm -f kanidmd kowhai-k >/dev/null 2>&1 || true; docker network rm $NET >/dev/null 2>&1 || true' EXIT

docker network create $NET >/dev/null
if ! docker image inspect "$IMG" >/dev/null 2>&1; then
  docker build -t "$IMG" .
fi

cat > "$WORK/server.toml" <<'EOF'
bindaddress = "[::]:8443"
db_path = "/data/kanidm.db"
tls_chain = "/w/chain.pem"
tls_key = "/w/key.pem"
domain = "kanidmd"
origin = "https://kanidmd:8443"
EOF
# kanidmd only serves HTTPS. It makes its own CA (ca.pem) and server certificate.
docker run --rm -v "$WORK:/w" -e KANIDM_CONFIG=/w/server.toml "$KANIDM" kanidmd cert-generate >/dev/null 2>&1
docker run --rm -v "$WORK:/w" alpine:3.22 chmod -R a+rwX /w

docker run -d --name kanidmd --network $NET -v "$WORK:/w" -e KANIDM_CONFIG=/w/server.toml \
  --tmpfs /data "$KANIDM" kanidmd server >/dev/null
for i in $(seq 1 60); do
  docker logs kanidmd 2>&1 | grep -q "Ready to rock" && break
  sleep 1
done

PW=$(docker exec kanidmd kanidmd recover-account idm_admin 2>&1 | sed -n 's/.*new_password: "\([^"]*\)".*/\1/p')
test -n "$PW"

kanidm() {
  docker run --rm --network $NET -v "$WORK:/w" -e KANIDM_PASSWORD="$PW" -e KANIDM_URL=https://kanidmd:8443 \
    -e KANIDM_CA_PATH=/w/ca.pem -v "$WORK/home:/root" "$TOOLS" kanidm "$@"
}
mkdir -p "$WORK/home"
kanidm login -D idm_admin
kanidm system oauth2 create kowhai Kowhai http://localhost:8080 -D idm_admin
kanidm system oauth2 add-redirect-url kowhai http://localhost:8080/auth/callback -D idm_admin
kanidm system oauth2 update-scope-map kowhai idm_all_persons openid profile email groups -D idm_admin
SECRET=$(kanidm system oauth2 show-basic-secret kowhai -D idm_admin | tail -n1 | tr -d '[:space:]')
test -n "$SECRET"

docker run -d --name kowhai-k --network $NET -v "$WORK:/w" \
  -e KOWHAI_BASE_URL=http://localhost:8080 \
  -e KOWHAI_OIDC_ISSUER=https://kanidmd:8443/oauth2/openid/kowhai \
  -e KOWHAI_OIDC_CLIENT_ID=kowhai -e KOWHAI_OIDC_CLIENT_SECRET="$SECRET" \
  -e KOWHAI_OIDC_CA_FILE=/w/ca.pem -e KOWHAI_DB_PATH=/tmp/kowhai.db "$IMG" >/dev/null
for i in $(seq 1 30); do
  docker exec kowhai-k wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 && break
  sleep 1
done

docker run --rm --network $NET -v "$WORK:/w" alpine:3.22 sh -euc '
  apk add --no-cache curl jq >/dev/null
  disc=$(curl -sf --cacert /w/ca.pem https://kanidmd:8443/oauth2/openid/kowhai/.well-known/openid-configuration)
  echo "$disc" | jq -e ".id_token_signing_alg_values_supported | index(\"ES256\")" >/dev/null
  echo "$disc" | jq -e ".code_challenge_methods_supported | index(\"S256\")" >/dev/null
  loc=$(curl -s -o /dev/null -w "%{redirect_url}" http://kowhai-k:8080/auth/login)
  case "$loc" in https://kanidmd:8443/ui/oauth2*code_challenge_method=S256*) ;; *) echo "bad redirect: $loc"; exit 1;; esac
  code=$(curl -s --cacert /w/ca.pem -o /dev/null -w "%{http_code}" "$loc")
  test "$code" = 200 || { echo "kanidm refused the request: $code"; exit 1; }
  echo "kanidm ok"
'
