#!/bin/bash
set -u

B=${BASE_URL:-http://localhost}
ADMIN_USER=${ADMIN_USERNAME:-admin}
ADMIN_PASS=${ADMIN_PASSWORD:-admin}
PASS=0; FAIL=0
AUTH=""

login() {
  local out code
  out=$(curl -s -w '\n%{http_code}' -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
  code=$(echo "$out" | tail -1)
  if [ "$code" != "200" ]; then
    echo "FAIL login as $ADMIN_USER (got $code)"
    FAIL=$((FAIL+1))
    return 1
  fi
  TOKEN=$(echo "$out" | sed '$d' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
  AUTH="Authorization: Bearer $TOKEN"
  PASS=$((PASS+1))
  echo "ok   [200] login as $ADMIN_USER"
}

check() {
  local name="$1" want="$2"
  shift 2
  local out code
  out=$(curl -s -w '\n%{http_code}' -H "$AUTH" "$@")
  code=$(echo "$out" | tail -1)
  body=$(echo "$out" | sed '$d')
  if [ "$code" = "$want" ]; then PASS=$((PASS+1)); echo "ok   [$code] $name"; else FAIL=$((FAIL+1)); echo "FAIL [$code want $want] $name"; echo "     body: $body"; fi
}

jid() { echo "$1" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*'; }

RUN=$(date +%s)
ORD_A="ORD-A-$RUN"
ORD_B="ORD-B-$RUN"

check_noauth() {
  local name="$1" want="$2"
  shift 2
  local out code
  out=$(curl -s -w '\n%{http_code}' "$@")
  code=$(echo "$out" | tail -1)
  body=$(echo "$out" | sed '$d')
  if [ "$code" = "$want" ]; then PASS=$((PASS+1)); echo "ok   [$code] $name"; else FAIL=$((FAIL+1)); echo "FAIL [$code want $want] $name"; echo "     body: $body"; fi
}

login || exit 1

OUT1=$(curl -s -w '\n%{http_code}' -H "$AUTH" -X POST "$B/api/v1/orders" -H 'Content-Type: application/json' -d "{\"orderNumber\":\"$ORD_A\"}")
CODE1=$(echo "$OUT1" | tail -1); BODY1=$(echo "$OUT1" | sed '$d'); ID1=$(jid "$BODY1")
OUT2=$(curl -s -w '\n%{http_code}' -H "$AUTH" -X POST "$B/api/v1/orders" -H 'Content-Type: application/json' -d "{\"orderNumber\":\"$ORD_B\",\"targetCompletionAt\":\"2026-09-10T12:00:00Z\"}")
CODE2=$(echo "$OUT2" | tail -1); BODY2=$(echo "$OUT2" | sed '$d'); ID2=$(jid "$BODY2")
[ "$CODE1" = "201" ] && { PASS=$((PASS+1)); echo "ok   [201] create order A ($ID1)"; } || { echo "FAIL create A"; FAIL=$((FAIL+1)); }
[ "$CODE2" = "201" ] && { PASS=$((PASS+1)); echo "ok   [201] create order B ($ID2)"; } || { echo "FAIL create B"; FAIL=$((FAIL+1)); }

check_noauth "unauthenticated list -> 401" 401 "$B/api/v1/orders"
check "duplicate order number" 409 -X POST "$B/api/v1/orders" -H 'Content-Type: application/json' -d "{\"orderNumber\":\"$ORD_A\"}"
check "missing order number" 400 -X POST "$B/api/v1/orders" -H 'Content-Type: application/json' -d '{}'
check "bad json body" 400 -X POST "$B/api/v1/orders" -H 'Content-Type: application/json' -d '{oops'
check "target in past" 400 -X POST "$B/api/v1/orders" -H 'Content-Type: application/json' -d '{"orderNumber":"ORD-X","targetCompletionAt":"2020-01-01T00:00:00Z"}'

check "list orders" 200 "$B/api/v1/orders"
check "list orders filtered" 200 "$B/api/v1/orders?status=Received"
check "list orders bad filter" 400 "$B/api/v1/orders?status=Bogus"
check "get order detail" 200 "$B/api/v1/orders/$ID1"
check "get missing order" 404 "$B/api/v1/orders/99999"

check "Received->Completed blocked" 409 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"Completed"}'
check "invalid status value" 400 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"Nope"}'
check "Received->Processing" 200 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"Processing"}'

OUTH=$(curl -s -w '\n%{http_code}' -H "$AUTH" -X POST "$B/api/v1/orders/$ID1/holds" -H 'Content-Type: application/json' -d '{"reason":"awaiting parts"}')
CODEH=$(echo "$OUTH" | tail -1); BODYH=$(echo "$OUTH" | sed '$d'); HID=$(jid "$BODYH")
[ "$CODEH" = "201" ] && { PASS=$((PASS+1)); echo "ok   [201] place hold ($HID)"; } || { echo "FAIL place hold"; FAIL=$((FAIL+1)); }
echo "   held order detail: $(curl -s -H "$AUTH" "$B/api/v1/orders/$ID1" | grep -o '"status":"[^"]*"')"
check "transition blocked while held" 409 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"QC_Review"}'
check "duplicate hold blocked" 409 -X POST "$B/api/v1/orders/$ID1/holds" -H 'Content-Type: application/json' -d '{"reason":"again"}'
OUT=$(curl -s -H "$AUTH" "$B/api/v1/orders?status=On_Hold")
echo "   On_Hold filter count: $(echo "$OUT" | grep -o '"orderNumber"' | wc -l)"
check "resolve hold" 200 -X POST "$B/api/v1/holds/$HID/resolve" -H 'Content-Type: application/json' -d '{}'
check "resolve again blocked" 409 -X POST "$B/api/v1/holds/$HID/resolve" -H 'Content-Type: application/json' -d '{}'
check "resolve missing hold" 404 -X POST "$B/api/v1/holds/99999/resolve" -d '{}'
check "Processing->QC_Review" 200 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"QC_Review"}'
check "QC fail" 201 -X POST "$B/api/v1/orders/$ID1/qc" -H 'Content-Type: application/json' -d '{"status":"FAIL","inspectorId":"insp-1","notes":"dimension mismatch"}'
check "complete blocked on fail" 409 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"Completed"}'
check "QC bad status" 400 -X POST "$B/api/v1/orders/$ID1/qc" -H 'Content-Type: application/json' -d '{"status":"MAYBE","inspectorId":"insp-1"}'
check "QC pass" 201 -X POST "$B/api/v1/orders/$ID1/qc" -H 'Content-Type: application/json' -d '{"status":"PASS","inspectorId":"insp-1","notes":"all good"}'
check "QC_Review->Completed" 200 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"Completed"}'
check "hold on completed blocked" 409 -X POST "$B/api/v1/orders/$ID1/holds" -H 'Content-Type: application/json' -d '{"reason":"late issue"}'
check "transition completed blocked" 409 -X POST "$B/api/v1/orders/$ID1/transition" -H 'Content-Type: application/json' -d '{"status":"Processing"}'
check "qc on completed blocked" 409 -X POST "$B/api/v1/orders/$ID1/qc" -H 'Content-Type: application/json' -d '{"status":"PASS","inspectorId":"insp-1"}'

check "audit trail" 200 "$B/api/v1/orders/$ID1/audit"
echo "   audit entries: $(curl -s -H "$AUTH" "$B/api/v1/orders/$ID1/audit" | grep -o '"action"' | wc -l)"
check "audit missing order" 404 "$B/api/v1/orders/99999/audit"

check "create user" 201 -X POST "$B/api/v1/users" -H 'Content-Type: application/json' -d "{\"username\":\"carol$RUN\",\"password\":\"password123\",\"displayName\":\"Carol\",\"role\":\"qc\"}"
check "duplicate user" 409 -X POST "$B/api/v1/users" -H 'Content-Type: application/json' -d "{\"username\":\"carol$RUN\",\"password\":\"password123\",\"role\":\"qc\"}"
check "unknown endpoint" 404 "$B/api/v1/nope"

echo
echo "--- attention, scanner, comments ---"

check "attention projection" 200 "$B/api/v1/attention"
check "scan match" 200 -X POST "$B/api/v1/scan" -H 'Content-Type: application/json' -d "{\"code\":\"$ORD_B\"}"
check "scan event recorded" 200 "$B/api/v1/orders/$ID2/scans"
echo "   scan events: $(curl -s -H "$AUTH" "$B/api/v1/orders/$ID2/scans" | grep -o '"scannedBy"' | wc -l)"
check "scan unknown code" 404 -X POST "$B/api/v1/scan" -H 'Content-Type: application/json' -d '{"code":"NOPE-000"}'
check "scan bad action" 400 -X POST "$B/api/v1/scan" -H 'Content-Type: application/json' -d "{\"code\":\"$ORD_B\",\"action\":\"DELETE\"}"
check_noauth "unauthenticated scan -> 401" 401 -X POST "$B/api/v1/scan" -H 'Content-Type: application/json' -d "{\"code\":\"$ORD_B\"}"

check "comments list" 200 "$B/api/v1/orders/$ID2/comments"
check "add comment with mention" 201 -X POST "$B/api/v1/orders/$ID2/comments" -H 'Content-Type: application/json' -d "{\"body\":\"ship it @admin and @ghost$RUN <script>alert(1)</script>\"}"
echo "   stored mentions: $(curl -s -H "$AUTH" "$B/api/v1/orders/$ID2/comments" | grep -o '"mentions":\["admin"\]' | wc -l)"
check "empty comment rejected" 400 -X POST "$B/api/v1/orders/$ID2/comments" -H 'Content-Type: application/json' -d '{"body":"   "}'
check "comment on missing order" 404 -X POST "$B/api/v1/orders/99999/comments" -H 'Content-Type: application/json' -d '{"body":"x"}'

OUTN=$(curl -s -w '\n%{http_code}' -H "$AUTH" "$B/api/v1/notifications")
CODEN=$(echo "$OUTN" | tail -1)
[ "$CODEN" = "200" ] && { PASS=$((PASS+1)); echo "ok   [200] notifications list (unread: $(echo "$OUTN" | sed '$d' | grep -o '"unread":[0-9]*' | grep -o '[0-9]*'))"; } || { echo "FAIL notifications list"; FAIL=$((FAIL+1)); }
NIDS=$(echo "$OUTN" | sed '$d' | grep -o '"id":[0-9]*' | grep -o '[0-9]*' | tr '\n' ',' | sed 's/,$//')
if [ -n "$NIDS" ]; then
  check "mark notifications read" 200 -X POST "$B/api/v1/notifications/read" -H 'Content-Type: application/json' -d "{\"ids\":[$NIDS]}"
fi
check "notifications unread empty" 200 "$B/api/v1/notifications?unread=true"

OUTH2=$(curl -s -w '\n%{http_code}' -H "$AUTH" -X POST "$B/api/v1/orders/$ID2/holds" -H 'Content-Type: application/json' -d '{"reason":"attention e2e"}')
[ "$(echo "$OUTH2" | tail -1)" = "201" ] && { PASS=$((PASS+1)); echo "ok   [201] hold order B for attention"; } || { echo "FAIL hold order B"; FAIL=$((FAIL+1)); }
echo "   attention contains held order: $(curl -s -H "$AUTH" "$B/api/v1/attention" | grep -c "\"orderNumber\":\"$ORD_B\"")"

echo
echo "PASS=$PASS FAIL=$FAIL"
exit $((FAIL > 0))
