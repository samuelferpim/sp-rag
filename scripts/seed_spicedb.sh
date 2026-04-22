#!/bin/bash
# =============================================================
# SP-RAG — SpiceDB Seed Data
# Creates test tenants, users, teams, documents, and relationships.
#
# Tenancy model:
#   - tenant:acme       users alice, bob, charlie (multi-tenant showcase)
#   - tenant:contoso    user diana (isolated tenant)
# A user can only view a document if they belong to its tenant.
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

info "Creating tenant memberships..."

# tenant:acme → alice (admin), bob + charlie (members)
write_rel tenant acme    admin  user alice
write_rel tenant acme    member user bob
write_rel tenant acme    member user charlie

# tenant:contoso → diana (admin). Isolated from tenant:acme.
write_rel tenant contoso admin  user diana

info "Creating team memberships..."

# Team memberships (scoped within tenants by convention):
#   finance_team: alice, charlie
#   eng_team:     bob, charlie
#   hr_team:      alice
#   legal_team:   diana
write_rel team finance_team member user alice
write_rel team finance_team member user charlie
write_rel team eng_team     member user bob
write_rel team eng_team     member user charlie
write_rel team hr_team      member user alice
write_rel team legal_team   member user diana

info "Creating document permissions..."

# ── tenant:acme documents ──────────────────────────────────────────
write_rel document relatorio_financeiro tenant tenant acme
write_rel document engineering_roadmap  tenant tenant acme
write_rel document hr_policy            tenant tenant acme

write_rel document relatorio_financeiro viewer team finance_team member
write_rel document engineering_roadmap  viewer team eng_team     member
write_rel document hr_policy            viewer team hr_team      member

write_rel document relatorio_financeiro owner user alice
write_rel document engineering_roadmap  owner user bob
write_rel document hr_policy            owner user alice

# ── tenant:contoso documents ───────────────────────────────────────
write_rel document contoso_legal_brief  tenant tenant contoso
write_rel document contoso_legal_brief  viewer team legal_team member
write_rel document contoso_legal_brief  owner  user diana

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
    '{consistency: {fullyConsistent: true}, resource: {objectType: "document", objectId: $ri}, permission: "view", subject: {object: {objectType: "user", objectId: $ui}}}')

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

# tenant:acme — intra-tenant access
check_perm relatorio_financeiro alice   allowed
check_perm engineering_roadmap  alice   denied
check_perm hr_policy            alice   allowed

check_perm relatorio_financeiro bob     denied
check_perm engineering_roadmap  bob     allowed
check_perm hr_policy            bob     denied

check_perm relatorio_financeiro charlie allowed
check_perm engineering_roadmap  charlie allowed
check_perm hr_policy            charlie denied

# tenant:contoso — cross-tenant isolation.
# diana is in legal_team but NOT a member of tenant:acme → MUST be denied on acme docs.
check_perm relatorio_financeiro diana   denied
check_perm engineering_roadmap  diana   denied
check_perm contoso_legal_brief  diana   allowed

# alice belongs to tenant:acme → MUST be denied on contoso docs even if she
# were somehow added to legal_team (cross-tenant intersection blocks it).
check_perm contoso_legal_brief  alice   denied

echo ""
echo "========================================="
echo -e "  ${GREEN}SpiceDB seed data applied!${NC}"
echo "========================================="
echo ""
