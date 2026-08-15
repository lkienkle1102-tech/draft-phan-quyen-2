CREATE TABLE users(id TEXT PRIMARY KEY);
CREATE TABLE organizations(id TEXT PRIMARY KEY,name TEXT NOT NULL);
CREATE TABLE permissions(id TEXT PRIMARY KEY,resource_type TEXT NOT NULL,action TEXT NOT NULL,UNIQUE(resource_type,action));

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
CREATE UNIQUE INDEX organization_members_one_active ON organization_members(organization_id,user_id) WHERE active=1;

CREATE TABLE organization_membership_applications(
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    status TEXT NOT NULL CHECK(status IN('pending','approved','rejected','cancelled')),
    reviewed_by TEXT REFERENCES users(id),
    created_at TEXT NOT NULL,
    reviewed_at TEXT
);
CREATE UNIQUE INDEX uq_pending_membership_application ON organization_membership_applications(organization_id,user_id) WHERE status='pending';

CREATE TABLE features_v2(key TEXT PRIMARY KEY,description TEXT NOT NULL DEFAULT '');
CREATE TABLE quota_definitions_v2(key TEXT PRIMARY KEY,reset_period TEXT NOT NULL CHECK(reset_period IN('none','daily','weekly','monthly','yearly')));
CREATE TABLE subject_feature_entitlements_v2(
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
CREATE TABLE plans_v2(
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
CREATE TABLE plan_feature_rules_v2(
    plan_id TEXT NOT NULL REFERENCES plans_v2(id) ON DELETE CASCADE,
    feature_key TEXT NOT NULL REFERENCES features_v2(key) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(plan_id,feature_key)
);
CREATE TABLE plan_quota_rules_v2(
    plan_id TEXT NOT NULL REFERENCES plans_v2(id) ON DELETE CASCADE,
    quota_key TEXT NOT NULL REFERENCES quota_definitions_v2(key) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    quota_limit INTEGER CHECK(quota_limit IS NULL OR quota_limit>=0),
    PRIMARY KEY(plan_id,quota_key)
);
CREATE TABLE subscriptions_v2(
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
CREATE TABLE subject_quota_entitlements_v2(
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
CREATE TABLE quota_reservations_v2(
    id TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    quota_entitlement_id TEXT NOT NULL REFERENCES subject_quota_entitlements_v2(id) ON DELETE CASCADE,
    amount INTEGER NOT NULL CHECK(amount>0),
    status TEXT NOT NULL CHECK(status IN('reserved','committed','released','expired')),
    expires_at TEXT NOT NULL,
    PRIMARY KEY(id,quota_entitlement_id)
);
CREATE TABLE quota_ledger_v2(
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

CREATE TABLE invoices(id TEXT PRIMARY KEY,organization_id TEXT NOT NULL REFERENCES organizations(id),owner_id TEXT NOT NULL REFERENCES users(id),approver_id TEXT REFERENCES users(id),status TEXT NOT NULL,amount INTEGER NOT NULL,region TEXT NOT NULL,system INTEGER NOT NULL DEFAULT 0,version INTEGER NOT NULL DEFAULT 1);
CREATE TABLE idempotency_records(key TEXT PRIMARY KEY,request_hash TEXT NOT NULL,response_json TEXT NOT NULL);
CREATE TABLE audit_events(id INTEGER PRIMARY KEY,actor_id TEXT,resource_id TEXT,action TEXT,policy_id TEXT,policy_version INTEGER);
CREATE TABLE audit_events_v2(
    id TEXT PRIMARY KEY,occurred_at TEXT NOT NULL,actor_id TEXT NOT NULL,client_id TEXT,
    subject_type TEXT,subject_id TEXT,resource_type TEXT,resource_id TEXT,action TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK(outcome IN('allow','deny','error')),reason TEXT NOT NULL,
    policy_id TEXT,policy_version INTEGER,metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE organization_invitations_v2(
    id TEXT PRIMARY KEY,organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK(status IN('pending','accepted','revoked','expired')),
    invited_by TEXT NOT NULL REFERENCES users(id),valid_from TEXT NOT NULL,valid_until TEXT NOT NULL,
    accepted_at TEXT,CHECK(valid_until>valid_from),UNIQUE(organization_id,user_id,status)
);
CREATE TABLE organization_invitation_roles_v2(
    invitation_id TEXT NOT NULL REFERENCES organization_invitations_v2(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL,PRIMARY KEY(invitation_id,role_id)
);

CREATE TABLE external_grants_v2(
    id TEXT PRIMARY KEY,owner_organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK(target_type IN('global_user','organization','organization_member')),
    target_user_id TEXT REFERENCES users(id) ON DELETE CASCADE,target_organization_id TEXT REFERENCES organizations(id) ON DELETE CASCADE,
    target_membership_id TEXT REFERENCES organization_members(id) ON DELETE CASCADE,resource_type TEXT NOT NULL,
    resource_id TEXT,action TEXT NOT NULL,effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    status TEXT NOT NULL CHECK(status IN('active','revoked','expired')),valid_from TEXT NOT NULL,valid_until TEXT,
    created_by TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,revoked_by TEXT REFERENCES users(id),revoked_at TEXT,
    CHECK(valid_until IS NULL OR valid_until>valid_from),
    CHECK((target_type='global_user' AND target_user_id IS NOT NULL AND target_organization_id IS NULL AND target_membership_id IS NULL) OR
          (target_type='organization' AND target_user_id IS NULL AND target_organization_id IS NOT NULL AND target_membership_id IS NULL) OR
          (target_type='organization_member' AND target_user_id IS NOT NULL AND target_organization_id IS NOT NULL AND target_membership_id IS NOT NULL))
);
CREATE INDEX external_grants_lookup_v2 ON external_grants_v2(owner_organization_id,resource_type,action,status,valid_from,valid_until);
CREATE TABLE external_grant_permissions_v2(grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,permission_id TEXT NOT NULL REFERENCES permissions(id),effect TEXT NOT NULL CHECK(effect IN('allow','deny')),PRIMARY KEY(grant_id,permission_id));
CREATE TABLE external_grant_roles_v2(grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,role_id TEXT NOT NULL,effect TEXT NOT NULL CHECK(effect IN('allow','deny')),PRIMARY KEY(grant_id,role_id));
CREATE TABLE external_grant_groups_v2(grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,group_id TEXT NOT NULL,effect TEXT NOT NULL CHECK(effect IN('allow','deny')),PRIMARY KEY(grant_id,group_id));
CREATE TABLE external_grant_features_v2(grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,feature_key TEXT NOT NULL REFERENCES features_v2(key),effect TEXT NOT NULL CHECK(effect IN('allow','deny')),source_type TEXT NOT NULL CHECK(source_type IN('explicit','plan')),source_id TEXT NOT NULL,PRIMARY KEY(grant_id,feature_key,source_type,source_id));
CREATE TABLE external_grant_plans_v2(grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,plan_id TEXT NOT NULL REFERENCES plans_v2(id),effect TEXT NOT NULL CHECK(effect IN('allow','deny')),PRIMARY KEY(grant_id,plan_id));
CREATE TABLE external_grant_quota_allocations_v2(
    grant_id TEXT NOT NULL REFERENCES external_grants_v2(id) ON DELETE CASCADE,quota_key TEXT NOT NULL REFERENCES quota_definitions_v2(key),
    owner_entitlement_id TEXT REFERENCES subject_quota_entitlements_v2(id),effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    allocated INTEGER NOT NULL CHECK(allocated>=0),used INTEGER NOT NULL DEFAULT 0 CHECK(used>=0),reserved INTEGER NOT NULL DEFAULT 0 CHECK(reserved>=0),
    source_type TEXT NOT NULL CHECK(source_type IN('explicit','plan')),source_id TEXT NOT NULL,CHECK(used+reserved<=allocated),
    PRIMARY KEY(grant_id,quota_key,owner_entitlement_id,source_type,source_id)
);
CREATE TABLE external_grant_quota_reservations_v2(operation_id TEXT NOT NULL,grant_id TEXT NOT NULL,quota_key TEXT NOT NULL,owner_entitlement_id TEXT NOT NULL,amount INTEGER NOT NULL CHECK(amount>0),status TEXT NOT NULL CHECK(status IN('reserved','committed','released','expired')),expires_at TEXT NOT NULL,PRIMARY KEY(operation_id,grant_id,quota_key,owner_entitlement_id));
CREATE TABLE external_grant_events_v2(id INTEGER PRIMARY KEY AUTOINCREMENT,grant_id TEXT NOT NULL REFERENCES external_grants_v2(id),actor_id TEXT NOT NULL,event TEXT NOT NULL CHECK(event IN('created','revoked','expired')),occurred_at TEXT NOT NULL);

CREATE TRIGGER subject_feature_entitlements_v2_subject_insert BEFORE INSERT ON subject_feature_entitlements_v2
WHEN (NEW.subject_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.subject_id)) OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.subject_id))
BEGIN SELECT RAISE(ABORT,'feature subject does not exist'); END;
CREATE TRIGGER subscriptions_v2_subject_insert BEFORE INSERT ON subscriptions_v2
WHEN (NEW.subject_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.subject_id)) OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.subject_id))
BEGIN SELECT RAISE(ABORT,'subscription subject does not exist'); END;
CREATE TRIGGER subject_quota_entitlements_v2_subject_insert BEFORE INSERT ON subject_quota_entitlements_v2
WHEN (NEW.subject_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.subject_id)) OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.subject_id))
BEGIN SELECT RAISE(ABORT,'quota subject does not exist'); END;
CREATE TRIGGER plans_v2_owner_insert BEFORE INSERT ON plans_v2
WHEN NEW.owner_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.owner_id)
BEGIN SELECT RAISE(ABORT,'plan owner does not exist'); END;
CREATE TRIGGER quota_reservations_v2_subject_insert BEFORE INSERT ON quota_reservations_v2
WHEN NOT EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 entitlement WHERE entitlement.id=NEW.quota_entitlement_id AND entitlement.subject_type=NEW.subject_type AND entitlement.subject_id=NEW.subject_id)
BEGIN SELECT RAISE(ABORT,'reservation subject does not match entitlement'); END;
CREATE TRIGGER external_grants_member_target_insert BEFORE INSERT ON external_grants_v2
WHEN NEW.target_type='organization_member' AND NOT EXISTS(SELECT 1 FROM organization_members member WHERE member.id=NEW.target_membership_id AND member.organization_id=NEW.target_organization_id AND member.user_id=NEW.target_user_id AND member.active=1)
BEGIN SELECT RAISE(ABORT,'target membership is not active or does not match'); END;
