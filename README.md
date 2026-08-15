# phan-quyen-golang

Authorization service in Go with Gin, Casdoor, Casbin, and SQLite. Casdoor is the required identity provider for users, sessions, MFA, tokens, and machine clients. Casbin owns direct, role, and group permissions with domain isolation and deny-wins. SQLite owns business state: application organizations and membership, plans, features, quotas, invitations, external grants, invoices, idempotency, and audit events.

## Requirements

- Go 1.25+
- A reachable Casdoor instance
- Node.js 22.12+ and npm
- Git and Make

Install the pinned quality tools and hooks:

```bash
make check-tools
make tools
npm install
```

## Casdoor bootstrap

1. Copy [casdoor/init_data.json.example](casdoor/init_data.json.example) to the Casdoor instance as `init_data.json`.
2. Replace every `CHANGE_ME_*` placeholder before importing it. The checked-in example contains no usable credential or key.
3. Export the application's public signing certificate from Casdoor.

The example creates one identity organization, the API application, the deny-wins domain-RBAC model, adapter, enforcer, stable roles/groups, and seed permissions equivalent to the local demo data. Application organizations such as `org-a` and `org-b` remain SQLite tenants and Casbin domains; they are not Casdoor organizations.

## Run locally

```bash
export CASDOOR_ENDPOINT=http://localhost:8000
export CASDOOR_CLIENT_ID=your-client-id
export CASDOOR_CLIENT_SECRET=your-client-secret
export CASDOOR_CERTIFICATE='-----BEGIN CERTIFICATE-----
...
-----END CERTIFICATE-----'
HTTP_ADDRESS=:8080 SQLITE_PATH=phan-quyen.db go run .
```

`HTTP_ADDRESS` defaults to `:8080`; `SQLITE_PATH` defaults to `phan-quyen.db`. This is a clean schema cutover: use a new SQLite database instead of a database created by the previous manual-IAM implementation. The parent directory must already exist.

| Variable | Default |
| --- | --- |
| `CASDOOR_ORGANIZATION` | `identity` |
| `CASDOOR_APPLICATION` | `authorization-api` |
| `CASDOOR_PERMISSION_ID` | `app-authorization` |
| `CASDOOR_MODEL_ID` | `application-domain-rbac` |
| `CASDOOR_RESOURCE_ID` | `application-policy-adapter` |
| `CASDOOR_ENFORCER_ID` | `application-enforcer` |
| `CASDOOR_OWNER` | `admin` |

`CASDOOR_CLIENT_ID`, `CASDOOR_CLIENT_SECRET`, and `CASDOOR_CERTIFICATE` have no defaults. Startup fails if any required Casdoor value is missing. Every request introspects its bearer token and fails closed on inactive/expired tokens, issuer or audience mismatch, timeout, parse failure, or Casdoor outage.

## API

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

Requests use Casdoor bearer tokens. Mutating invoice requests also require `Idempotency-Key`.

## Identity and authorization model

Principals use `user::<id>`, `machine::<client_id>`, `role::<stable-id>`, and `group::<stable-id>`. Domains use `user::<id>` or `organization::<id>`. A local active membership remains a hard gate, so a Casbin role cannot create membership or grant cross-tenant access.

The static endpoint catalog is deployed with the binary. Hard checks preserve actor type, selected scope, tenant/resource ownership, system-resource protection, MFA and recent-auth requirements, membership, and external-grant resolution. Casbin then evaluates permission; SQLite evaluates feature and quota facts; deterministic behavior rules enrich the decision with strategy and obligations.

Each request selects exactly one personal or organization entitlement scope. Facts from different scopes are never merged. Explicit external access evaluates only the immutable bundle issued by the resource-owning organization. Deny wins inside each scope.

External grants have three distinct targets:

- `global_user` follows the global identity without depending on another organization's membership.
- `organization_member` binds to one membership ID; kick and rejoin do not reactivate the old grant.
- `organization` follows current active membership; kick disables access and rejoin restores it.

Grant plans materialize feature and quota items into the immutable bundle. Quota is reserved from the owner's allocation, atomically consumed with business state, and returned when unused capacity is revoked. `external_grant.manage` cannot be delegated.

Invitation bundles store stable Casdoor role IDs. Acceptance adds only missing Casbin `g` policies, writes membership in SQLite, and compensates only policies created by that attempt if the transaction fails. The membership hard gate prevents privilege leakage even if compensation fails.

`GET /v1/me` merges a fail-closed raw Casbin snapshot for roles, groups, and permissions with local feature, plan, quota, membership, and external-grant data. Sources and effects remain scope-separated; empty collections serialize as arrays.

## Quality gates

```bash
make test
make check-all
```

`make check-all` runs formatting, unit/integration tests, the race detector, vet, lint, layout and architecture checks, duplicate-code checks, and compilation. Commit messages must use English Conventional Commit format with a scope and description.
