# phan-quyen-golang

Authorization service in Go with Gin and SQLite. The runtime implements strict personal/organization scope selection, hard endpoint contracts, data-driven soft policies, deny-wins grants, plan-derived feature/quota entitlements, atomic quota accounting, invitation-based organization membership, behavior/obligation routing, idempotency, and authorization audit events.

## Requirements

- Go 1.25+
- Node.js 22.12+ and npm
- Git and Make

Install the pinned quality tools and hooks:

```bash
make check-tools
make tools
npm install
```

## Run locally

```bash
HTTP_ADDRESS=:8080 SQLITE_PATH=phan-quyen.db JWT_SECRET=development-secret go run .
```

`HTTP_ADDRESS` defaults to `:8080`; `SQLITE_PATH` defaults to `phan-quyen.db`. The database is migrated and seeded during startup. Its parent directory must already exist. SQLite enables foreign keys, a five-second busy timeout, and WAL mode for file-backed databases.

The protected sample routes are:

- `GET /v1/me`
- `POST /v1/organizations/:organizationID/invoices/:invoiceID/approve`
- `POST /v1/organizations/:organizationID/membership-applications`
- `POST /v1/organizations/:organizationID/membership-applications/:applicationID/review`
- `POST /v1/organizations/:organizationID/membership-invitations`
- `POST /v1/membership-invitations/accept`
- `POST /v1/organizations/:organizationID/external-user-grants`
- `GET /v1/organizations/:organizationID/external-user-grants`
- `DELETE /v1/organizations/:organizationID/external-user-grants/:grantID`

Requests use an HS256 JWT. Mutating invoice requests also require `Idempotency-Key`.

`GET /v1/me` returns the authenticated user identity, personal entitlements, each active organization membership with its independently evaluated entitlements, and currently applicable external grants. Allow and deny sources remain visible, with deny winning only inside the same scope. Personal, organization, and external-grant facts are never merged. This response is a current context snapshot for clients; resource ownership, request attributes, assurance, policy, and quota cost are still authorized by each business endpoint.

## Authorization model

Each request resolves exactly one entitlement subject: the personal user or the organization selected by the resource route. Personal and foreign-organization entitlements are never merged into the selected organization. An endpoint marked `explicit_organization_grant` may instead evaluate only an immutable bundle issued by the resource-owning organization. `strict_isolation` endpoints never accept that bundle. Explicit deny wins across matching grants and across direct permissions, roles, groups, features, plans, quotas, and policy denial trees.

External grants have three intentionally different targets:

- `global_user`: follows the global user identity and does not depend on membership in another organization.
- `organization_member`: binds to one membership ID. A kick disables it immediately, and a later rejoin creates a new membership ID, so the old grant stays disabled.
- `organization`: applies to all active members of the target organization. A kick disables access; a rejoin restores access while the grant itself remains active.

Grant plans are templates: their feature and quota rules are materialized into the immutable grant. Explicit grant items can tighten the result, and deny wins. Allocated quota is reserved from the resource owner's quota at creation, consumed by external operations with reserve/commit/release, and unused capacity is returned on revoke. The `external_grant.manage` capability is internal-only and cannot itself be delegated.

Plan activation materializes time-bounded feature and quota entitlements. Add-ons and manual overrides remain independent records. Quota use is rechecked and reserved inside the business transaction, then committed, released, or expired with an immutable ledger. Middleware performs authentication, endpoint resolution, hard and soft authorization, and decision auditing; application services own state invariants and execute the selected strategy and obligations.

## Quality gates

```bash
make test
make check-all
```

`make check-all` runs formatting, unit/integration tests, the race detector, vet, lint, layout and architecture checks, duplicate-code checks, and compilation. Commit messages must use English Conventional Commit format with a scope and description.
