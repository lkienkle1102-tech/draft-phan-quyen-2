PRAGMA foreign_keys=OFF;

ALTER TABLE organization_members RENAME TO organization_members_legacy;

CREATE TABLE organization_members(
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN(0,1)),
    joined_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    left_at TEXT,
    CHECK((active=1 AND left_at IS NULL) OR active=0),
    CHECK(left_at IS NULL OR left_at>=joined_at)
);
CREATE UNIQUE INDEX organization_members_one_active
ON organization_members(organization_id,user_id) WHERE active=1;

INSERT INTO organization_members(id,organization_id,user_id,active,joined_at,left_at)
SELECT 'legacy:'||organization_id||':'||user_id,organization_id,user_id,active,
       '1970-01-01T00:00:00Z',CASE WHEN active=0 THEN CURRENT_TIMESTAMP END
FROM organization_members_legacy;
DROP TABLE organization_members_legacy;

DROP TRIGGER IF EXISTS role_assignments_v2_subject_insert;
DROP TRIGGER IF EXISTS subject_permission_rules_v2_subject_insert;
CREATE TRIGGER role_assignments_v2_subject_insert
BEFORE INSERT ON role_assignments_v2
WHEN (NEW.subject_type='user' AND NEW.subject_id<>NEW.user_id)
  OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organization_members WHERE organization_id=NEW.subject_id AND user_id=NEW.user_id AND active=1))
BEGIN SELECT RAISE(ABORT,'role assignment subject is invalid'); END;
CREATE TRIGGER subject_permission_rules_v2_subject_insert
BEFORE INSERT ON subject_permission_rules_v2
WHEN (NEW.subject_type='user' AND NEW.subject_id<>NEW.user_id)
  OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organization_members WHERE organization_id=NEW.subject_id AND user_id=NEW.user_id AND active=1))
BEGIN SELECT RAISE(ABORT,'permission subject is invalid'); END;

CREATE TRIGGER organization_members_integrity_insert
BEFORE INSERT ON organization_members
WHEN NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.organization_id)
  OR NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.user_id)
BEGIN SELECT RAISE(ABORT,'membership parent does not exist'); END;

CREATE TABLE external_grants_v2(
    id TEXT PRIMARY KEY,
    owner_organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK(target_type IN('global_user','organization','organization_member')),
    target_user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    target_organization_id TEXT REFERENCES organizations(id) ON DELETE CASCADE,
    target_membership_id TEXT REFERENCES organization_members(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    action TEXT NOT NULL,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    status TEXT NOT NULL CHECK(status IN('active','revoked','expired')),
    valid_from TEXT NOT NULL,
    valid_until TEXT,
    created_by TEXT NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    revoked_by TEXT REFERENCES users(id),
    revoked_at TEXT,
    CHECK(valid_until IS NULL OR valid_until>valid_from),
    CHECK(
      (target_type='global_user' AND target_user_id IS NOT NULL AND target_organization_id IS NULL AND target_membership_id IS NULL) OR
      (target_type='organization' AND target_user_id IS NULL AND target_organization_id IS NOT NULL AND target_membership_id IS NULL) OR
      (target_type='organization_member' AND target_user_id IS NOT NULL AND target_organization_id IS NOT NULL AND target_membership_id IS NOT NULL)
    )
);
CREATE INDEX external_grants_lookup_v2 ON external_grants_v2(owner_organization_id,resource_type,action,status,valid_from,valid_until);

CREATE TRIGGER external_grants_member_target_insert
BEFORE INSERT ON external_grants_v2 WHEN NEW.target_type='organization_member' AND NOT EXISTS(
  SELECT 1 FROM organization_members m WHERE m.id=NEW.target_membership_id
    AND m.organization_id=NEW.target_organization_id AND m.user_id=NEW.target_user_id AND m.active=1
)
BEGIN SELECT RAISE(ABORT,'target membership is not active or does not match'); END;

CREATE TABLE external_grant_permissions_v2(
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES permissions(id),
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(grant_id,permission_id)
);
CREATE TABLE external_grant_roles_v2(
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles_v2(id),
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(grant_id,role_id)
);
CREATE TABLE external_grant_groups_v2(
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,
    group_id TEXT NOT NULL REFERENCES groups_v2(id),
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(grant_id,group_id)
);
CREATE TABLE external_grant_features_v2(
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,
    feature_key TEXT NOT NULL REFERENCES features_v2(key),
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    source_type TEXT NOT NULL CHECK(source_type IN('explicit','plan')),
    source_id TEXT NOT NULL,
    PRIMARY KEY(grant_id,feature_key,source_type,source_id)
);
CREATE TABLE external_grant_plans_v2(
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,
    plan_id TEXT NOT NULL REFERENCES plans_v2(id),
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(grant_id,plan_id)
);
CREATE TABLE external_grant_quota_allocations_v2(
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,
    quota_key TEXT NOT NULL REFERENCES quota_definitions_v2(key),
    owner_entitlement_id TEXT REFERENCES subject_quota_entitlements_v2(id),
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    allocated INTEGER NOT NULL CHECK(allocated>=0),
    used INTEGER NOT NULL DEFAULT 0 CHECK(used>=0),
    reserved INTEGER NOT NULL DEFAULT 0 CHECK(reserved>=0),
    source_type TEXT NOT NULL CHECK(source_type IN('explicit','plan')),
    source_id TEXT NOT NULL,
    CHECK(used+reserved<=allocated),
    PRIMARY KEY(grant_id,quota_key,owner_entitlement_id,source_type,source_id)
);
CREATE TABLE external_grant_quota_reservations_v2(
    operation_id TEXT NOT NULL,
    grant_id TEXT NOT NULL,
    quota_key TEXT NOT NULL,
    owner_entitlement_id TEXT NOT NULL,
    amount INTEGER NOT NULL CHECK(amount>0),
    status TEXT NOT NULL CHECK(status IN('reserved','committed','released','expired')),
    expires_at TEXT NOT NULL,
    PRIMARY KEY(operation_id,grant_id,quota_key,owner_entitlement_id)
);
CREATE TABLE external_grant_events_v2(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id),
    actor_id TEXT NOT NULL,
    event TEXT NOT NULL CHECK(event IN('created','revoked','expired')),
    occurred_at TEXT NOT NULL
);

INSERT OR IGNORE INTO permissions(id,resource_type,action) VALUES('external_grant.manage','external_grant','manage');
INSERT OR IGNORE INTO role_permission_rules_v2(role_id,permission_id,effect)
SELECT 'organization:org-a:finance','external_grant.manage','allow'
WHERE EXISTS(SELECT 1 FROM roles_v2 WHERE id='organization:org-a:finance');

INSERT OR IGNORE INTO authorization_policies(id,version,active) VALUES('external-grant-manage',1,1);
INSERT OR IGNORE INTO policy_nodes(id,policy_id,policy_version,parent_id,node_type,rule_type,config_json,position,purpose) VALUES
('root','external-grant-manage',1,NULL,'RULE','permission','{}',0,'allow');
INSERT OR IGNORE INTO endpoint_bindings(id,method,route_template,resource_loader,intent_resolver,resource_type,action,policy_id,policy_version,active,scope_mode,allow_personal_fallback) VALUES
('external-grant-create','POST','/v1/organizations/:organizationID/external-user-grants','external-grant-owner','external-grant-manage','external_grant','manage','external-grant-manage',1,1,'organization',0),
('external-grant-list','GET','/v1/organizations/:organizationID/external-user-grants','external-grant-owner','external-grant-manage','external_grant','manage','external-grant-manage',1,1,'organization',0),
('external-grant-revoke','DELETE','/v1/organizations/:organizationID/external-user-grants/:grantID','external-grant-owner','external-grant-manage','external_grant','manage','external-grant-manage',1,1,'organization',0);

PRAGMA foreign_keys=ON;
