CREATE TRIGGER roles_v2_owner_insert
BEFORE INSERT ON roles_v2
WHEN (NEW.owner_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.owner_id))
  OR (NEW.owner_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.owner_id))
BEGIN
    SELECT RAISE(ABORT,'role owner does not exist');
END;

CREATE TRIGGER roles_v2_owner_update
BEFORE UPDATE OF owner_type,owner_id ON roles_v2
WHEN (NEW.owner_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.owner_id))
  OR (NEW.owner_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.owner_id))
BEGIN
    SELECT RAISE(ABORT,'role owner does not exist');
END;

CREATE TRIGGER role_assignments_v2_subject_insert
BEFORE INSERT ON role_assignments_v2
WHEN (NEW.subject_type='user' AND NEW.subject_id<>NEW.user_id)
  OR (NEW.subject_type='organization' AND NOT EXISTS(
      SELECT 1 FROM organization_members
      WHERE organization_id=NEW.subject_id AND user_id=NEW.user_id AND active=1
  ))
BEGIN
    SELECT RAISE(ABORT,'role assignment subject is invalid');
END;

CREATE TRIGGER subject_permission_rules_v2_subject_insert
BEFORE INSERT ON subject_permission_rules_v2
WHEN (NEW.subject_type='user' AND NEW.subject_id<>NEW.user_id)
  OR (NEW.subject_type='organization' AND NOT EXISTS(
      SELECT 1 FROM organization_members
      WHERE organization_id=NEW.subject_id AND user_id=NEW.user_id AND active=1
  ))
BEGIN
    SELECT RAISE(ABORT,'permission subject is invalid');
END;

CREATE TRIGGER subject_feature_entitlements_v2_subject_insert
BEFORE INSERT ON subject_feature_entitlements_v2
WHEN (NEW.subject_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.subject_id))
  OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.subject_id))
BEGIN
    SELECT RAISE(ABORT,'feature subject does not exist');
END;

CREATE TRIGGER subscriptions_v2_subject_insert
BEFORE INSERT ON subscriptions_v2
WHEN (NEW.subject_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.subject_id))
  OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.subject_id))
BEGIN
    SELECT RAISE(ABORT,'subscription subject does not exist');
END;

CREATE TRIGGER subject_quota_entitlements_v2_subject_insert
BEFORE INSERT ON subject_quota_entitlements_v2
WHEN (NEW.subject_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.subject_id))
  OR (NEW.subject_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.subject_id))
BEGIN
    SELECT RAISE(ABORT,'quota subject does not exist');
END;

CREATE TRIGGER groups_v2_owner_insert
BEFORE INSERT ON groups_v2
WHEN (NEW.owner_type='user' AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.owner_id))
  OR (NEW.owner_type='organization' AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.owner_id))
BEGIN
    SELECT RAISE(ABORT,'group owner does not exist');
END;

CREATE TRIGGER plans_v2_owner_insert
BEFORE INSERT ON plans_v2
WHEN NEW.owner_type='organization'
 AND NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.owner_id)
BEGIN
    SELECT RAISE(ABORT,'plan owner does not exist');
END;

CREATE TRIGGER quota_reservations_v2_subject_insert
BEFORE INSERT ON quota_reservations_v2
WHEN NOT EXISTS(
    SELECT 1 FROM subject_quota_entitlements_v2 entitlement
    WHERE entitlement.id=NEW.quota_entitlement_id
      AND entitlement.subject_type=NEW.subject_type
      AND entitlement.subject_id=NEW.subject_id
)
BEGIN
    SELECT RAISE(ABORT,'reservation subject does not match entitlement');
END;

CREATE TRIGGER organization_members_integrity_insert
BEFORE INSERT ON organization_members
WHEN NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.organization_id)
  OR NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.user_id)
BEGIN
    SELECT RAISE(ABORT,'membership parent does not exist');
END;

CREATE TRIGGER invoices_integrity_insert
BEFORE INSERT ON invoices
WHEN NOT EXISTS(SELECT 1 FROM organizations WHERE id=NEW.organization_id)
  OR NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.owner_id)
  OR (NEW.approver_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM users WHERE id=NEW.approver_id))
BEGIN
    SELECT RAISE(ABORT,'invoice parent does not exist');
END;

CREATE TRIGGER organization_access_grants_v2_forbid_insert
BEFORE INSERT ON organization_access_grants
WHEN NEW.status='active'
BEGIN
    SELECT RAISE(ABORT,'cross-organization grants are disabled by strict isolation');
END;

CREATE TRIGGER organization_access_grants_v2_forbid_update
BEFORE UPDATE OF status ON organization_access_grants
WHEN NEW.status='active'
BEGIN
    SELECT RAISE(ABORT,'cross-organization grants are disabled by strict isolation');
END;
