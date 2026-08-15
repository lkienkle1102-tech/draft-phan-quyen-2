CREATE TABLE groups_v2(
    id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL CHECK(owner_type IN('user','organization')),
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN(0,1)),
    UNIQUE(owner_type,owner_id,name)
);

CREATE TABLE group_memberships_v2(
    group_id TEXT NOT NULL REFERENCES groups_v2(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    valid_from TEXT NOT NULL,
    valid_until TEXT,
    PRIMARY KEY(group_id,user_id),
    CHECK(valid_until IS NULL OR valid_until>valid_from)
);

CREATE TABLE group_role_rules_v2(
    group_id TEXT NOT NULL REFERENCES groups_v2(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles_v2(id) ON DELETE CASCADE,
    effect TEXT NOT NULL CHECK(effect IN('allow','deny')),
    PRIMARY KEY(group_id,role_id)
);

CREATE TRIGGER group_role_rules_v2_owner_insert
BEFORE INSERT ON group_role_rules_v2
WHEN NOT EXISTS(
    SELECT 1 FROM groups_v2 group_row
    JOIN roles_v2 role ON role.id=NEW.role_id
    WHERE group_row.id=NEW.group_id
      AND group_row.owner_type=role.owner_type
      AND group_row.owner_id=role.owner_id
)
BEGIN
    SELECT RAISE(ABORT,'group and role owners differ');
END;

CREATE TABLE policy_denials_v2(
    policy_id TEXT NOT NULL,
    policy_version INTEGER NOT NULL,
    root_node_id TEXT NOT NULL,
    denial_code TEXT NOT NULL CHECK(denial_code IN(
        'hard_contract_denied','permission_denied','feature_disabled','quota_exceeded',
        'tenant_mismatch','policy_invalid','organization_grant_required',
        'organization_membership_required','plan_unavailable'
    )),
    PRIMARY KEY(policy_id,policy_version,root_node_id),
    FOREIGN KEY(policy_id,policy_version,root_node_id)
        REFERENCES policy_nodes(policy_id,policy_version,id) ON DELETE CASCADE
);

INSERT INTO groups_v2(id,owner_type,owner_id,name,active)
SELECT subject_type||':'||subject_id||':'||id,subject_type,subject_id,name,1
FROM groups;

INSERT INTO group_memberships_v2(group_id,user_id,effect,valid_from)
SELECT group_row.subject_type||':'||group_row.subject_id||':'||member.group_id,
       member.user_id,'allow','1970-01-01T00:00:00Z'
FROM group_members member
JOIN groups group_row ON group_row.id=member.group_id;

INSERT INTO group_role_rules_v2(group_id,role_id,effect)
SELECT group_row.subject_type||':'||group_row.subject_id||':'||assignment.group_id,
       group_row.subject_type||':'||group_row.subject_id||':'||assignment.role_id,
       'allow'
FROM group_roles assignment
JOIN groups group_row ON group_row.id=assignment.group_id
WHERE EXISTS(
    SELECT 1 FROM roles_v2 role
    WHERE role.id=group_row.subject_type||':'||group_row.subject_id||':'||assignment.role_id
);
