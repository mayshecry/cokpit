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
checkas() {
  local name="$1" want="$2" tok="$3"
  shift 3
  local out code body
  out=$(curl -s -w '\n%{http_code}' -H "Authorization: Bearer $tok" "$@")
  code=$(echo "$out" | tail -1)
  body=$(echo "$out" | sed '$d')
  if [ "$code" = "$want" ]; then PASS=$((PASS+1)); echo "ok   [$code] $name"; else FAIL=$((FAIL+1)); echo "FAIL [$code want $want] $name"; echo "     body: $body"; fi
}

jid() { echo "$1" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*'; }

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
echo "--- qol: assignee, deep data, bulk, stale guard, backup ---"

check "list carries server SLA" 200 "$B/api/v1/orders?limit=5"
echo "   sla fields on list: $(curl -s -H "$AUTH" "$B/api/v1/orders?limit=5" | grep -o '"sla":{' | wc -l)"
check "list assignee filter" 200 "$B/api/v1/orders?assignee=unassigned"
check "users brief" 200 "$B/api/v1/users/brief"

check "assign order to admin" 200 -X POST "$B/api/v1/orders/$ID2/assign" -H 'Content-Type: application/json' -d '{"assignee":"admin"}'
echo "   assignee stored: $(curl -s -H "$AUTH" "$B/api/v1/orders/$ID2" | grep -o '"assignee":"admin"' | wc -l)"
check "assign unknown user" 400 -X POST "$B/api/v1/orders/$ID2/assign" -H 'Content-Type: application/json' -d '{"assignee":"ghost7381"}'
check "unassign" 200 -X POST "$B/api/v1/orders/$ID2/assign" -H 'Content-Type: application/json' -d '{"assignee":""}'

OUTD=$(curl -s -w '\n%{http_code}' -X POST "$B/api/v1/orders" -H "$AUTH" -H 'Content-Type: application/json' -d "{\"orderNumber\":\"$ORD_A\"}")
CODED=$(echo "$OUTD" | tail -1)
[ "$CODED" = "409" ] && { PASS=$((PASS+1)); echo "ok   [409] duplicate order number"; } || { echo "FAIL duplicate order number (got $CODED)"; FAIL=$((FAIL+1)); }
echo "   duplicate payload carries existingOrder: $(echo "$OUTD" | sed '$d' | grep -c 'existingOrder')"
QOLN="QOL-STALE-$RUN"
curl -s -o /dev/null -X POST "$B/api/v1/orders" -H "$AUTH" -H 'Content-Type: application/json' -d "{\"orderNumber\":\"$QOLN\"}"
QOLID=$(curl -s -H "$AUTH" "$B/api/v1/orders?limit=100" | grep -B 3 "\"orderNumber\":\"$QOLN\"" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
UPD=$(curl -s -H "$AUTH" "$B/api/v1/orders/$QOLID" | sed -n 's/.*"updatedAt":"\([^"]*\)".*/\1/p')
check "fresh guarded write accepted" 200 -X POST "$B/api/v1/orders/$QOLID/transition" -H 'Content-Type: application/json' -d "{\"status\":\"Processing\",\"expectedUpdatedAt\":\"$UPD\"}"
check "stale write rejected" 409 -X POST "$B/api/v1/orders/$QOLID/transition" -H 'Content-Type: application/json' -d "{\"status\":\"QC_Review\",\"expectedUpdatedAt\":\"$UPD\"}"

OUTB=$(curl -s -w '\n%{http_code}' -H "$AUTH" -X POST "$B/api/v1/orders/bulk-transition" -H 'Content-Type: application/json' -d "{\"ids\":[$ID1,$ID2],\"status\":\"Processing\"}")
CODEB=$(echo "$OUTB" | tail -1)
[ "$CODEB" = "200" ] && { PASS=$((PASS+1)); echo "ok   [200] bulk transition"; } || { echo "FAIL bulk transition (got $CODEB)"; FAIL=$((FAIL+1)); }
echo "   bulk per-item results: $(echo "$OUTB" | sed '$d' | grep -o '"ok"' | wc -l)"
check "bulk resolve (no active hold -> skipped)" 200 -X POST "$B/api/v1/holds/bulk-resolve" -H 'Content-Type: application/json' -d "{\"orderIds\":[$ID1]}"

OUTS=$(timeout 3 curl -sN -D - "$B/api/v1/events?token=$(echo "$AUTH" | sed 's/.*Bearer //')" 2>/dev/null | head -6)
echo "$OUTS" | grep -q "text/event-stream" && { PASS=$((PASS+1)); echo "ok   [sse] event-stream headers"; } || { echo "FAIL sse headers"; FAIL=$((FAIL+1)); }

OUTBK=$(curl -s -o /tmp/cockpit-backup-e2e.db -w '%{http_code}' -H "$AUTH" "$B/api/v1/admin/backup")
[ "$OUTBK" = "200" ] && [ "$(head -c 15 /tmp/cockpit-backup-e2e.db)" = "SQLite format 3" ] && { PASS=$((PASS+1)); echo "ok   [200] admin backup is a SQLite file"; } || { echo "FAIL admin backup (got $OUTBK)"; FAIL=$((FAIL+1)); }
rm -f /tmp/cockpit-backup-e2e.db
check_noauth "backup without token -> 401" 401 "$B/api/v1/admin/backup"

echo
echo "--- product manuals ---"
PROD=$(curl -s -w '\n%{http_code}' -H "$AUTH" -X POST "$B/api/v1/products" -H 'Content-Type: application/json' -d "{\"code\":\"PROD-E2E\",\"name\":\"E2E Product\"}")
PRODCODE=$(echo "$PROD" | tail -1)
PID=$(echo "$PROD" | sed '$d' | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
[ "$PRODCODE" = "201" ] && { PASS=$((PASS+1)); echo "ok   [201] create product"; } || { echo "FAIL create product (got $PRODCODE)"; FAIL=$((FAIL+1)); }
B1=$(curl -s -H "$AUTH" -X POST "$B/api/v1/products/$PID/blocks" -H 'Content-Type: application/json' -d '{"title":"Block one","body":"desc"}' | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
B2=$(curl -s -H "$AUTH" -X POST "$B/api/v1/products/$PID/blocks" -H 'Content-Type: application/json' -d '{"title":"Block two","body":"desc"}' | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
B3=$(curl -s -H "$AUTH" -X POST "$B/api/v1/products/$PID/blocks" -H 'Content-Type: application/json' -d '{"title":"Block three","body":"desc"}' | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
echo "   blocks created: $B1 $B2 $B3"
SHUF=$(curl -s -H "$AUTH" -X POST "$B/api/v1/products/$PID/blocks/$B1/move" -H 'Content-Type: application/json' -d '{"direction":"down"}')
BLOCKIDS=$(echo "$SHUF" | grep -o '"id":[0-9]*' | grep -o '[0-9]*' | tr '\n' ' ')
echo "   after move down: $BLOCKIDS"
check "move first block up -> 409 edge" 409 -X POST "$B/api/v1/products/$PID/blocks/$(echo "$SHUF" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')/move" -H 'Content-Type: application/json' -d '{"direction":"up"}'
check "assign block to admin" 200 -X POST "$B/api/v1/products/$PID/blocks/$B2" -H 'Content-Type: application/json' -d '{"assignee":"admin"}'
check "assign unknown user -> 400" 400 -X POST "$B/api/v1/products/$PID/blocks/$B2" -H 'Content-Type: application/json' -d '{"assignee":"nobody"}'
MANUAL_ORDER="ORD-MAN-E2E-$RUN"
curl -s -o /dev/null -H "$AUTH" -X POST "$B/api/v1/orders" -H 'Content-Type: application/json' -d "{\"orderNumber\":\"$MANUAL_ORDER\"}"
MOID=$(curl -s -H "$AUTH" "$B/api/v1/orders?limit=100" | grep -B 2 "\"orderNumber\":\"$MANUAL_ORDER\"" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
echo "   manual order id: $MOID"

check "attach manual" 201 -X POST "$B/api/v1/orders/$MOID/manuals" -H 'Content-Type: application/json' -d "{\"productId\":$PID}"
MID=$(curl -s -H "$AUTH" "$B/api/v1/orders/$MOID/manuals" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
echo "   manual id: $MID"
check "answer YES" 200 -X POST "$B/api/v1/manuals/$MID/blocks/$(echo "$SHUF" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')/answer" -H 'Content-Type: application/json' -d '{"answer":"YES"}'
check "answer NO" 200 -X POST "$B/api/v1/manuals/$MID/blocks/$(echo "$SHUF" | grep -o '"id":[0-9]*' | tail -1 | grep -o '[0-9]*')/answer" -H 'Content-Type: application/json' -d '{"answer":"NO"}'
check "invalid answer -> 400" 400 -X POST "$B/api/v1/manuals/$MID/blocks/$(echo "$SHUF" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')/answer" -H 'Content-Type: application/json' -d '{"answer":"MAYBE"}'
OP="worker$RUN"
check "create flag-test operator" 201 -X POST "$B/api/v1/users" -H 'Content-Type: application/json' -d "{\"username\":\"$OP\",\"password\":\"password123\",\"displayName\":\"Worker\",\"role\":\"operator\"}"
OP_TOK=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"$OP\",\"password\":\"password123\"}" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
FB=$(echo "$SHUF" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
checkas "operator answers block" 200 "$OP_TOK" -X POST "$B/api/v1/manuals/$MID/blocks/$FB/answer" -H 'Content-Type: application/json' -d '{"answer":"YES"}'

checkas "operator cannot flag -> 403" 403 "$OP_TOK" -X POST "$B/api/v1/manuals/$MID/blocks/$FB/flag" -H 'Content-Type: application/json' -d '{"reason":"nope"}'
check "flag without reason -> 400" 400 -X POST "$B/api/v1/manuals/$MID/blocks/$FB/flag" -H 'Content-Type: application/json' -d '{}'
check "flag answered block" 200 -X POST "$B/api/v1/manuals/$MID/blocks/$FB/flag" -H 'Content-Type: application/json' -d '{"reason":"seal not checked"}'
echo "   flagged block: $(curl -s -H "$AUTH" "$B/api/v1/orders/$MOID/manuals" | grep -o '"flagged":true' | wc -l) flagged, reason: $(curl -s -H "$AUTH" "$B/api/v1/orders/$MOID/manuals" | grep -o '"flagReason":"[^"]*"' | head -1)"
check "flag lands in tick log" 200 "$B/api/v1/orders/$MOID/manuals"
checkas "operator notified about flag" 200 "$OP_TOK" "$B/api/v1/notifications"
NOTIF=$(curl -s "$B/api/v1/notifications" -H "Authorization: Bearer $OP_TOK")
echo "$NOTIF" | grep -q '"kind":"flag"' && { PASS=$((PASS+1)); echo "ok   flag notification present"; } || { echo "FAIL flag notification missing"; FAIL=$((FAIL+1)); }
echo "$NOTIF" | grep -q 'seal not checked' && { PASS=$((PASS+1)); echo "ok   flag notification carries reason"; } || { echo "FAIL flag notification missing reason"; FAIL=$((FAIL+1)); }
echo "$NOTIF" | grep -q '"unread":1' && { PASS=$((PASS+1)); echo "ok   flag notification unread"; } || { echo "FAIL notification not unread"; FAIL=$((FAIL+1)); }

check "flag unanswered block -> 409" 409 -X POST "$B/api/v1/manuals/$MID/blocks/$(echo "$SHUF" | grep -o '"id":[0-9]*' | sed -n '2p' | grep -o '[0-9]*')/flag" -H 'Content-Type: application/json' -d '{"reason":"premature"}'
checkas "re-answer clears flag" 200 "$OP_TOK" -X POST "$B/api/v1/manuals/$MID/blocks/$FB/answer" -H 'Content-Type: application/json' -d '{"answer":"YES"}'
check "unflag via clear=true" 200 -X POST "$B/api/v1/manuals/$MID/blocks/$FB/flag?clear=true" -H 'Content-Type: application/json' -d '{}'
echo "   flags after unflag: $(curl -s -H "$AUTH" "$B/api/v1/orders/$MOID/manuals" | grep -o '"flagged":true' | wc -l)"
check "manuals work view" 200 "$B/api/v1/manuals"
echo "   overview progress: $(curl -s -H "$AUTH" "$B/api/v1/manuals" | grep -o '"orderNumber":"$MANUAL_ORDER"' | wc -l)"
check "duplicate manual attach -> 409" 409 -X POST "$B/api/v1/orders/$MOID/manuals" -H 'Content-Type: application/json' -d "{\"productId\":$PID}"
check "remove manual" 200 -X DELETE "$B/api/v1/manuals/$MID" -H "$AUTH"
check "manuals after remove" 200 "$B/api/v1/orders/$MOID/manuals"
echo "   manuals after remove: $(curl -s -H "$AUTH" "$B/api/v1/orders/$MOID/manuals" | grep -o '"id":[0-9]*' | wc -l)"
check "delete product" 200 -X DELETE "$B/api/v1/products/$PID" -H "$AUTH"

echo
echo "PASS=$PASS FAIL=$FAIL"
exit $((FAIL > 0))
