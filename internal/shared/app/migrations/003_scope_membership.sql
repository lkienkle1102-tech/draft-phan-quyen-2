ALTER TABLE endpoint_bindings ADD COLUMN scope_mode TEXT NOT NULL DEFAULT 'organization' CHECK(scope_mode IN('user','organization','either'));
ALTER TABLE endpoint_bindings ADD COLUMN allow_personal_fallback INTEGER NOT NULL DEFAULT 0 CHECK(allow_personal_fallback IN(0,1));
ALTER TABLE organization_access_grants ADD COLUMN grantee_user_id TEXT;

CREATE TABLE organization_membership_applications(
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    status TEXT NOT NULL CHECK(status IN('pending','approved','rejected','cancelled')),
    reviewed_by TEXT REFERENCES users(id),
    created_at TEXT NOT NULL,
    reviewed_at TEXT
);

CREATE UNIQUE INDEX uq_pending_membership_application
ON organization_membership_applications(organization_id,user_id)
WHERE status='pending';

CREATE INDEX idx_access_grant_member
ON organization_access_grants(grantee_organization_id,grantee_user_id,status);
