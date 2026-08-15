CREATE TABLE IF NOT EXISTS features_v2(
    key TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS quota_definitions_v2(
    key TEXT PRIMARY KEY,
    reset_period TEXT NOT NULL CHECK(reset_period IN('none','daily','weekly','monthly','yearly'))
);

ALTER TABLE policy_nodes RENAME TO policy_nodes_legacy;

CREATE TABLE policy_nodes(
    id TEXT NOT NULL,
    policy_id TEXT NOT NULL,
    policy_version INTEGER NOT NULL,
    parent_id TEXT,
    node_type TEXT NOT NULL CHECK(node_type IN('ALL','ANY','NOT','RULE')),
    rule_type TEXT,
    config_json TEXT NOT NULL DEFAULT '{}',
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(policy_id,policy_version,id),
    FOREIGN KEY(policy_id,policy_version)
        REFERENCES authorization_policies(id,version) ON DELETE CASCADE,
    FOREIGN KEY(policy_id,policy_version,parent_id)
        REFERENCES policy_nodes(policy_id,policy_version,id) ON DELETE CASCADE
        DEFERRABLE INITIALLY DEFERRED
);

INSERT INTO policy_nodes(id,policy_id,policy_version,parent_id,node_type,rule_type,config_json,position)
SELECT id,policy_id,policy_version,parent_id,node_type,rule_type,config_json,position
FROM policy_nodes_legacy;

DROP TABLE policy_nodes_legacy;

CREATE TABLE IF NOT EXISTS roles_v2(
    id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL CHECK(owner_type IN('user','organization')),
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN(0,1)),
    UNIQUE(owner_type,owner_id,name)
);

CREATE TABLE IF NOT EXISTS role_assignments_v2(
    subject_type TEXT NOT NULL CHECK(subject_type IN('user','organization')),
    subject_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles_v2(id) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    valid_from TEXT NOT NULL,
    valid_until TEXT,
    PRIMARY KEY(subject_type,subject_id,user_id,role_id),
    CHECK(valid_until IS NULL OR valid_until>valid_from)
);

CREATE TRIGGER IF NOT EXISTS role_assignments_v2_owner_insert
BEFORE INSERT ON role_assignments_v2
WHEN NOT EXISTS(
    SELECT 1 FROM roles_v2 role
    WHERE role.id=NEW.role_id
      AND role.owner_type=NEW.subject_type
      AND role.owner_id=NEW.subject_id
)
BEGIN
    SELECT RAISE(ABORT,'role owner does not match assignment subject');
END;

CREATE TRIGGER IF NOT EXISTS role_assignments_v2_owner_update
BEFORE UPDATE ON role_assignments_v2
WHEN NOT EXISTS(
    SELECT 1 FROM roles_v2 role
    WHERE role.id=NEW.role_id
      AND role.owner_type=NEW.subject_type
      AND role.owner_id=NEW.subject_id
)
BEGIN
    SELECT RAISE(ABORT,'role owner does not match assignment subject');
END;

CREATE TABLE IF NOT EXISTS role_permission_rules_v2(
    role_id TEXT NOT NULL REFERENCES roles_v2(id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(role_id,permission_id)
);

CREATE TABLE IF NOT EXISTS subject_permission_rules_v2(
    subject_type TEXT NOT NULL CHECK(subject_type IN('user','organization')),
    subject_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    valid_from TEXT NOT NULL,
    valid_until TEXT,
    PRIMARY KEY(subject_type,subject_id,user_id,permission_id),
    CHECK(valid_until IS NULL OR valid_until>valid_from)
);

CREATE TABLE IF NOT EXISTS subject_feature_entitlements_v2(
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK(subject_type IN('user','organization')),
    subject_id TEXT NOT NULL,
    feature_key TEXT NOT NULL REFERENCES features_v2(key) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    source_type TEXT NOT NULL CHECK(source_type IN('plan','addon','trial','manual')),
    source_id TEXT NOT NULL,
    valid_from TEXT NOT NULL,
    valid_until TEXT,
    CHECK(valid_until IS NULL OR valid_until>valid_from),
    UNIQUE(subject_type,subject_id,feature_key,source_type,source_id)
);

CREATE TABLE IF NOT EXISTS plans_v2(
    id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL CHECK(owner_type IN('system','organization')),
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN(0,1)),
    valid_from TEXT NOT NULL,
    valid_until TEXT,
    CHECK(valid_until IS NULL OR valid_until>valid_from),
    UNIQUE(owner_type,owner_id,name)
);

CREATE TABLE IF NOT EXISTS plan_feature_rules_v2(
    plan_id TEXT NOT NULL REFERENCES plans_v2(id) ON DELETE CASCADE,
    feature_key TEXT NOT NULL REFERENCES features_v2(key) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(plan_id,feature_key)
);

CREATE TABLE IF NOT EXISTS plan_quota_rules_v2(
    plan_id TEXT NOT NULL REFERENCES plans_v2(id) ON DELETE CASCADE,
    quota_key TEXT NOT NULL REFERENCES quota_definitions_v2(key) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    quota_limit INTEGER,
    CHECK(quota_limit IS NULL OR quota_limit>=0),
    PRIMARY KEY(plan_id,quota_key)
);

CREATE TABLE IF NOT EXISTS subscriptions_v2(
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK(subject_type IN('user','organization')),
    subject_id TEXT NOT NULL,
    plan_id TEXT NOT NULL REFERENCES plans_v2(id),
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    status TEXT NOT NULL CHECK(status IN('pending','trialing','active','past_due','cancelled','expired')),
    valid_from TEXT NOT NULL,
    valid_until TEXT,
    current_period_start TEXT NOT NULL,
    current_period_end TEXT NOT NULL,
    CHECK(current_period_end>current_period_start),
    CHECK(valid_until IS NULL OR valid_until>valid_from),
    UNIQUE(subject_type,subject_id)
);

CREATE TABLE IF NOT EXISTS subject_quota_entitlements_v2(
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK(subject_type IN('user','organization')),
    subject_id TEXT NOT NULL,
    quota_key TEXT NOT NULL REFERENCES quota_definitions_v2(key) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    quota_limit INTEGER,
    used INTEGER NOT NULL DEFAULT 0 CHECK(used>=0),
    reserved INTEGER NOT NULL DEFAULT 0 CHECK(reserved>=0),
    period_start TEXT NOT NULL,
    period_end TEXT,
    source_type TEXT NOT NULL CHECK(source_type IN('plan','addon','trial','manual')),
    source_id TEXT NOT NULL,
    CHECK(quota_limit IS NULL OR used+reserved<=quota_limit),
    CHECK(period_end IS NULL OR period_end>period_start),
    UNIQUE(subject_type,subject_id,quota_key,period_start,source_type,source_id)
);

CREATE TABLE IF NOT EXISTS quota_reservations_v2(
    id TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    quota_entitlement_id TEXT NOT NULL REFERENCES subject_quota_entitlements_v2(id) ON DELETE CASCADE,
    amount INTEGER NOT NULL CHECK(amount>0),
    status TEXT NOT NULL CHECK(status IN('reserved','committed','released','expired')),
    expires_at TEXT NOT NULL,
    PRIMARY KEY(id,quota_entitlement_id)
);

CREATE TABLE IF NOT EXISTS quota_ledger_v2(
    id INTEGER PRIMARY KEY,
    idempotency_key TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    action TEXT NOT NULL,
    quota_key TEXT NOT NULL REFERENCES quota_definitions_v2(key),
    amount INTEGER NOT NULL CHECK(amount>0),
    operation TEXT NOT NULL CHECK(operation IN('reserve','commit','release','expire','consume','refund','adjust')),
    occurred_at TEXT NOT NULL,
    UNIQUE(idempotency_key,actor_id,subject_type,subject_id,resource_type,action,quota_key,operation)
);

CREATE TABLE IF NOT EXISTS policy_behaviors_v2(
    policy_id TEXT NOT NULL,
    policy_version INTEGER NOT NULL,
    id TEXT NOT NULL,
    condition_root_id TEXT NOT NULL,
    strategy TEXT NOT NULL,
    priority INTEGER NOT NULL,
    parameters_json TEXT NOT NULL DEFAULT '{}',
    obligations_json TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY(policy_id,policy_version,id),
    FOREIGN KEY(policy_id,policy_version) REFERENCES authorization_policies(id,version) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS audit_events_v2(
    id TEXT PRIMARY KEY,
    occurred_at TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    client_id TEXT,
    subject_type TEXT,
    subject_id TEXT,
    resource_type TEXT,
    resource_id TEXT,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK(outcome IN('allow','deny','error')),
    reason TEXT NOT NULL,
    policy_id TEXT,
    policy_version INTEGER,
    metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS organization_invitations_v2(
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK(status IN('pending','accepted','revoked','expired')),
    invited_by TEXT NOT NULL REFERENCES users(id),
    valid_from TEXT NOT NULL,
    valid_until TEXT NOT NULL,
    accepted_at TEXT,
    CHECK(valid_until>valid_from),
    UNIQUE(organization_id,user_id,status)
);

CREATE TABLE IF NOT EXISTS organization_invitation_roles_v2(
    invitation_id TEXT NOT NULL REFERENCES organization_invitations_v2(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles_v2(id) ON DELETE CASCADE,
    PRIMARY KEY(invitation_id,role_id)
);

CREATE TRIGGER IF NOT EXISTS organization_invitation_roles_v2_owner_insert
BEFORE INSERT ON organization_invitation_roles_v2
WHEN NOT EXISTS(
    SELECT 1 FROM organization_invitations_v2 invitation
    JOIN roles_v2 role ON role.id=NEW.role_id
    WHERE invitation.id=NEW.invitation_id
      AND role.owner_type='organization'
      AND role.owner_id=invitation.organization_id
)
BEGIN
    SELECT RAISE(ABORT,'invitation role belongs to another subject');
END;

UPDATE endpoint_bindings
SET active=0
WHERE scope_mode NOT IN('user','organization');

UPDATE endpoint_bindings SET allow_personal_fallback=0;
UPDATE organization_access_grants SET status='revoked' WHERE status='active';

CREATE TRIGGER IF NOT EXISTS endpoint_bindings_v2_scope_insert
BEFORE INSERT ON endpoint_bindings
WHEN NEW.scope_mode NOT IN('user','organization') OR NEW.allow_personal_fallback<>0
BEGIN
    SELECT RAISE(ABORT,'endpoint binding requires one selected scope');
END;

CREATE TRIGGER IF NOT EXISTS endpoint_bindings_v2_scope_update
BEFORE UPDATE ON endpoint_bindings
WHEN NEW.scope_mode NOT IN('user','organization') OR NEW.allow_personal_fallback<>0
BEGIN
    SELECT RAISE(ABORT,'endpoint binding requires one selected scope');
END;

INSERT OR IGNORE INTO features_v2(key)
SELECT DISTINCT feature_key FROM subject_features;
INSERT OR IGNORE INTO quota_definitions_v2(key,reset_period)
SELECT DISTINCT quota_key,'monthly' FROM quota_counters;

INSERT OR IGNORE INTO roles_v2(id,owner_type,owner_id,name,active)
SELECT assignment.subject_type||':'||assignment.subject_id||':'||role.id,
       assignment.subject_type,assignment.subject_id,role.name,1
FROM roles role
JOIN subject_user_roles assignment ON assignment.role_id=role.id
GROUP BY assignment.subject_type,assignment.subject_id,role.id,role.name;

INSERT OR IGNORE INTO role_permission_rules_v2(role_id,permission_id,effect)
SELECT role.owner_type||':'||role.owner_id||':'||permission.role_id,
       permission.permission_id,'allow'
FROM role_permissions permission
JOIN roles_v2 role ON role.id=role.owner_type||':'||role.owner_id||':'||permission.role_id;

INSERT OR IGNORE INTO role_assignments_v2(subject_type,subject_id,user_id,role_id,effect,valid_from)
SELECT assignment.subject_type,assignment.subject_id,assignment.user_id,
       assignment.subject_type||':'||assignment.subject_id||':'||assignment.role_id,
       'allow','1970-01-01T00:00:00Z'
FROM subject_user_roles assignment
JOIN roles_v2 role
  ON role.id=assignment.subject_type||':'||assignment.subject_id||':'||assignment.role_id
WHERE role.owner_type=assignment.subject_type AND role.owner_id=assignment.subject_id;

INSERT OR IGNORE INTO subject_permission_rules_v2(subject_type,subject_id,user_id,permission_id,effect,valid_from)
SELECT subject_type,subject_id,user_id,permission_id,effect,'1970-01-01T00:00:00Z'
FROM user_permission_overrides;

INSERT OR IGNORE INTO subject_feature_entitlements_v2(
    id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from
)
SELECT subject_type||':'||subject_id||':'||feature_key,
       subject_type,subject_id,feature_key,
       CASE WHEN enabled=1 THEN 'allow' ELSE 'deny' END,
       'manual','legacy','1970-01-01T00:00:00Z'
FROM subject_features;

INSERT OR IGNORE INTO subject_quota_entitlements_v2(
    id,subject_type,subject_id,quota_key,effect,quota_limit,used,reserved,
    period_start,period_end,source_type,source_id
)
SELECT subject_type||':'||subject_id||':'||quota_key,
       subject_type,subject_id,quota_key,'allow',quota_limit,used,reserved,
       '1970-01-01T00:00:00Z',NULL,'manual','legacy'
FROM quota_counters
WHERE used>=0 AND reserved>=0
  AND (quota_limit IS NULL OR used+reserved<=quota_limit);
