#!/bin/sh
# CI check for the dev stack: signs in through Authelia with curl, then
# uploads and downloads an attachment through Garage. Run it inside the
# compose network:
#   docker run --rm --network kowhai_default -v $PWD/deploy/test:/t:ro alpine:3.22 sh /t/authelia-flow.sh
set -eu
apk add --no-cache curl jq >/dev/null
J=/tmp/jar
C="--connect-to auth.localhost:9091:authelia:9091"
K=http://kowhai:8080
loc=$(curl -s -c $J -b $J -o /dev/null -w '%{redirect_url}' "$K/auth/login")
echo "1 authorize: ${loc%%\?*}"
# Authelia sends us to its login page. Sign in through its API.
curl -sk $C -c $J -b $J -o /dev/null -w "authorize %{http_code} -> %{redirect_url}\n" "$loc"
curl -sk $C -c $J -b $J -H 'Content-Type: application/json' -d '{"username":"alice","password":"kowhai","keepMeLoggedIn":false}' https://auth.localhost:9091/api/firstfactor | jq -c .status
cb=$(curl -sk $C -c $J -b $J -o /dev/null -w '%{redirect_url}' "$loc")
echo "2 callback: ${cb%%\?*}"
cb=$(echo "$cb" | sed 's#http://localhost:8080#http://kowhai:8080#')
curl -s -c $J -b $J -o /dev/null -w '3 callback status %{http_code} -> %{redirect_url}\n' "$cb"
test "$(curl -s -b $J $K/api/v1/me | jq -r '.user.email + " " + (.user.is_admin|tostring)')" = "alice@example.com true"
CS=$(grep kowhai_csrf $J | awk '{print $7}')
H="X-Kowhai-CSRF: $CS"
ws=$(curl -s -b $J -H "$H" -H 'Content-Type: application/json' -d '{"name":"Docker"}' $K/api/v1/workspaces | jq -r .id)
b=$(curl -s -b $J -H "$H" -H 'Content-Type: application/json' -d '{"name":"B"}' $K/api/v1/workspaces/$ws/boards | jq -r .id)
l=$(curl -s -b $J -H "$H" -H 'Content-Type: application/json' -d '{"name":"L"}' $K/api/v1/boards/$b/lists | jq -r .id)
c=$(curl -s -b $J -H "$H" -H 'Content-Type: application/json' -d '{"title":"C"}' $K/api/v1/lists/$l/cards | jq -r .id)
echo "hello from garage" > /tmp/hello.txt
url=$(curl -s -b $J -H "$H" -F file=@/tmp/hello.txt $K/api/v1/cards/$c/attachments | jq -r .url)
echo "uploaded: $url"

test "$(curl -s -b $J $K$url)" = "hello from garage"
echo "ok"
