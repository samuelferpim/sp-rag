#!/bin/bash
# =============================================================
# SP-RAG — SpiceDB Seed Data
# Creates test users, teams, documents, and relationships
# Usage: make spicedb-seed
# =============================================================

set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

SPICEDB_HTTP="${SPICEDB_HTTP_ENDPOINT:-http://localhost:8443}"
TOKEN="${SPICEDB_PRESHARED_KEY:-sprag_dev_key}"

pass() { echo -e "  ${GREEN}✓${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; exit 1; }
info() { echo -e "\n${YELLOW}▸${NC} $1"; }

api() {
  local path=$1
  local data=$2
  curl -sf -X POST "${SPICEDB_HTTP}${path}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$data"
}

echo ""
echo "========================================="
echo "  SP-RAG — SpiceDB Seed Data"
echo "========================================="

# --- Write Schema ---
info "Writing permission schema..."

SCHEMA=$(cat infra/spicedb/schema.zed)
SCHEMA_JSON=$(jq -n --arg schema "$SCHEMA" '{schema: $schema}')

if api "/v1/schema/write" "$SCHEMA_JSON" > /dev/null; then
  pass "Schema applied"
else
  fail "Failed to write schema"
fi

# --- Create Relationships ---
info "Creating team memberships..."

write_rel() {
  local resource_type=$1
  local resource_id=$2
  local relation=$3
  local subject_type=$4
  local subject_id=$5
  local subject_relation=${6:-""}

  local subject
  if [ -n "$subject_relation" ]; then
    subject=$(jq -n \
      --arg st "$subject_type" \
      --arg si "$subject_id" \
      --arg sr "$subject_relation" \
      '{object: {objectType: $st, objectId: $si}, optionalRelation: $sr}')
  else
    subject=$(jq -n \
      --arg st "$subject_type" \
      --arg si "$subject_id" \
      '{object: {objectType: $st, objectId: $si}}')
  fi

  local body
  body=$(jq -n \
    --arg rt "$resource_type" \
    --arg ri "$resource_id" \
    --arg rel "$relation" \
    --argjson subj "$subject" \
    '{updates: [{operation: "OPERATION_TOUCH", relationship: {resource: {objectType: $rt, objectId: $ri}, relation: $rel, subject: $subj}}]}')

  if api "/v1/relationships/write" "$body" > /dev/null; then
    pass "$resource_type:$resource_id#$relation@$subject_type:$subject_id"
  else
    fail "Failed: $resource_type:$resource_id#$relation@$subject_type:$subject_id"
  fi
}

# Team memberships:
#   finance_team: alice, charlie
#   eng_team:     bob, charlie
#   hr_team:      alice
write_rel team finance_team member user alice
write_rel team finance_team member user charlie
write_rel team eng_team     member user bob
write_rel team eng_team     member user charlie
write_rel team hr_team      member user alice

info "Creating document permissions..."

# Documents with team-based viewer access:
#   relatorio_financeiro → viewer: finance_team (alice, charlie can view)
#   engineering_roadmap  → viewer: eng_team     (bob, charlie can view)
#   hr_policy            → viewer: hr_team      (alice can view)
write_rel document relatorio_financeiro viewer team finance_team member
write_rel document engineering_roadmap  viewer team eng_team     member
write_rel document hr_policy            viewer team hr_team      member

# Document owners:
write_rel document relatorio_financeiro owner user alice
write_rel document engineering_roadmap  owner user bob
write_rel document hr_policy            owner user alice

# --- Verify Permissions ---
info "Verifying permissions..."

check_perm() {
  local resource_id=$1
  local user_id=$2
  local expected=$3

  local body
  body=$(jq -n \
    --arg ri "$resource_id" \
    --arg ui "$user_id" \
    '{resource: {objectType: "document", objectId: $ri}, permission: "view", subject: {object: {objectType: "user", objectId: $ui}}}')

  local result
  result=$(api "/v1/permissions/check" "$body")
  local perm
  perm=$(echo "$result" | jq -r '.permissionship')

  if [ "$expected" = "allowed" ] && [ "$perm" = "PERMISSIONSHIP_HAS_PERMISSION" ]; then
    pass "document:$resource_id view user:$user_id → ALLOWED"
  elif [ "$expected" = "denied" ] && [ "$perm" != "PERMISSIONSHIP_HAS_PERMISSION" ]; then
    pass "document:$resource_id view user:$user_id → DENIED (expected)"
  else
    fail "document:$resource_id view user:$user_id → unexpected: $perm (expected: $expected)"
  fi
}

# alice: finance_team + hr_team + owner of relatorio_financeiro, hr_policy
check_perm relatorio_financeiro alice   allowed
check_perm engineering_roadmap  alice   denied
check_perm hr_policy            alice   allowed

# bob: eng_team + owner of engineering_roadmap
check_perm relatorio_financeiro bob     denied
check_perm engineering_roadmap  bob     allowed
check_perm hr_policy            bob     denied

# charlie: finance_team + eng_team (no ownership)
check_perm relatorio_financeiro charlie allowed
check_perm engineering_roadmap  charlie allowed
check_perm hr_policy            charlie denied

echo ""
echo "========================================="
echo -e "  ${GREEN}SpiceDB seed data applied!${NC}"
echo "========================================="
echo ""
