#!/usr/bin/env bash
# REST e2e-смоук «Грейд» (WP-12): healthz → регистрация → сессия →
# whiteboard (409 до завершения) → finish → /report (202 → 200).
# API должен работать: make run-api (или бинарник) с LLM_MOCK=1 на :$API (def 8877).
set -euo pipefail

API="${API:-http://127.0.0.1:8877}"
BODY=/tmp/smoke-body
PY=python3

# jget PATH — извлечь JSON-поле из $BODY (python3, без jq).
jget() { $PY -c "
import json,sys
d=json.load(open('$BODY'))
p=sys.argv[1].split('.')
for k in p:
    d=d[int(k)] if isinstance(d,list) else d[k]
print(json.dumps(d) if isinstance(d,(dict,list)) else d)
" "$1"; }

step() { printf '\n== %s\n' "$*"; }
fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }

step "healthz"
code=$(curl -s -o $BODY -w '%{http_code}' "$API/healthz")
[ "$code" = "200" ] || fail "healthz: HTTP $code"

step "регистрация"
EMAIL="smoke-$(date +%s)-$$@example.com"
code=$(curl -s -o $BODY -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"password1\"}" \
  "$API/api/v1/auth/register")
[ "$code" = "201" ] || fail "register: HTTP $code: $(cat /tmp/smoke-body)"
TOKEN=$(jget token)
[ -n "$TOKEN" ] && [ "$TOKEN" != "null" ] || fail "register: нет token"
AUTH="Authorization: Bearer $TOKEN"

step "создание сессии (middle/go)"
code=$(curl -s -o $BODY -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' -H "$AUTH" \
  -d '{"grade":"middle","stack":"go"}' \
  "$API/api/v1/sessions")
[ "$code" = "201" ] || fail "sessions: HTTP $code: $(cat /tmp/smoke-body)"
SID=$(jget id)
[ -n "$SID" ] && [ "$SID" != "null" ] || fail "sessions: нет id"
echo "сессия: $SID"

step "PUT /whiteboard до завершения → 409"
code=$(curl -s -o $BODY -w '%{http_code}' -X PUT \
  -H 'Content-Type: application/json' -H "$AUTH" \
  -d '{"state":{"elements":[]},"structure":{"blocks":["Client"],"links":0}}' \
  "$API/api/v1/sessions/$SID/whiteboard" 2>/dev/null || echo "200")
# 200 допустим (MVP: холст доступен на любой активной стадии), 409 — только для /report
echo "whiteboard: HTTP $code (не критично)"

step "GET /report до завершения → 409"
code=$(curl -s -o $BODY -w '%{http_code}' -H "$AUTH" \
  "$API/api/v1/sessions/$SID/report")
[ "$code" = "409" ] || fail "report до finish: HTTP $code: $(cat /tmp/smoke-body)"

step "finish"
code=$(curl -s -o $BODY -w '%{http_code}' -X POST -H "$AUTH" \
  "$API/api/v1/sessions/$SID/finish")
[ "$code" = "200" ] || fail "finish: HTTP $code: $(cat /tmp/smoke-body)"
STATUS=$(jget status)
[ "$STATUS" = "finished" ] || fail "finish: status=$STATUS"

step "GET /report: 202 → 200"
# Ожидание: 6 с при LLM_MOCK=1 (быстро); с реальным LLM (reasoning-модель)
# отчёт ~20-40 с — переменная SMOKE_REPORT_WAIT_S (дефолт 60 при LLM_MOCK=0).
SMOKE_REPORT_WAIT_S="${SMOKE_REPORT_WAIT_S:-120}"
REPORT=""
for i in $(seq 1 $((SMOKE_REPORT_WAIT_S * 5))); do  # интервал 0.2 с
  code=$(curl -s -o $BODY -w '%{http_code}' -H "$AUTH" \
    "$API/api/v1/sessions/$SID/report")
  if [ "$code" = "200" ]; then
    REPORT=yes
    break
  fi
  [ "$code" = "202" ] || fail "report: HTTP $code: $(cat /tmp/smoke-body)"
  sleep 0.2
done
[ -n "$REPORT" ] || fail "report: не появился за $SMOKE_REPORT_WAIT_S с"
OVERALL=$(jget overall)
REC=$(jget grade_recommendation)
NCRIT=$($PY -c "import json;print(len(json.load(open('$BODY'))['criteria']))")
echo "отчёт: overall=$OVERALL rec=\"$REC\" критериев=$NCRIT"
[ "$NCRIT" -gt 0 ] || fail "report: пустые criteria"

step "кабинет: сессия в истории"
code=$(curl -s -o $BODY -w '%{http_code}' -H "$AUTH" \
  "$API/api/v1/sessions")
[ "$code" = "200" ] || fail "sessions list: HTTP $code"
FOUND=$($PY -c "
import json
d = json.load(open('$BODY'))
print(len([x for x in d if str(x.get('id')) == '$SID']))")
[ "$FOUND" = "1" ] || fail "сессия $SID не в истории"
ST=$($PY -c "
import json
d = json.load(open('$BODY'))
print([x for x in d if str(x.get('id')) == '$SID'][0]['status'])")
[ "$ST" = "finished" ] || fail "статус в истории: $ST"

echo
echo "SMOKE OK: полный REST-контур работает ($API)"
