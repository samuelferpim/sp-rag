# ADR-004: SpiceDB for Zero-Trust Access Control

## Status
Accepted

## Context
RAG systems that return document chunks to users must enforce access control. A naive RAG returns any document matching the query, regardless of who uploaded it or who should see it. This is a critical security flaw in enterprise environments where documents contain sensitive financial, HR, or legal information.

We need a system that:
1. Models document-level permissions (owner, viewer, team-based access)
2. Evaluates permissions at query time with sub-millisecond latency
3. Follows a fail-closed principle (deny on error)
4. Is versionable and auditable

## Decision
Use SpiceDB for authorization, implementing the Google Zanzibar model.

Permission schema:
```
definition user {}
definition team { relation member: user }
definition document {
    relation owner: user
    relation viewer: user | team#member
    permission view = owner + viewer
}
```

Defense-in-depth strategy:
1. **Pre-filter:** Qdrant query includes payload filter matching user's teams or ownership (coarse, fast)
2. **Post-check:** Each returned document is verified via SpiceDB `CheckPermission` (fine-grained, authoritative)

Access is denied if SpiceDB is unreachable (fail-closed).

## Alternatives Considered
- **Custom RBAC middleware:** Simpler initially, but becomes fragile as permission complexity grows. No graph-based evaluation, hard to reason about transitive permissions (team membership → document access).
- **Open Policy Agent (OPA):** Excellent for policy evaluation, but not designed for relationship-based access control. OPA evaluates rules, not graphs. Would require custom data loading for relationship queries.
- **Casbin:** Lightweight Go library, but lacks the graph-based model needed for team→member→document traversal. Better suited for simple RBAC/ABAC.

## Consequences
**Positive:**
- Google Zanzibar model is battle-tested at scale (Google Drive, YouTube, etc.)
- Schema is versionable and can be tested independently
- gRPC-native with excellent Go SDK (`authzed/authzed-go`)
- Fail-closed by design prevents unauthorized data exposure
- Defense-in-depth: even if Qdrant filtering has a bug, SpiceDB catches unauthorized access

**Negative:**
- Additional infrastructure component (gRPC service)
- Relationships must be written during document upload (additional write path)
- Adds latency to query path (~5-15ms for CheckPermission per unique document)
- Requires careful schema design to avoid permission explosion
