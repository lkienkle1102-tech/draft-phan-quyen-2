INSERT OR IGNORE INTO permissions(id,resource_type,action) VALUES
('membership.invite','organization_membership','invite'),
('membership.accept','organization_membership','accept');

INSERT OR REPLACE INTO role_permission_rules_v2(role_id,permission_id,effect)
SELECT id,'membership.invite','allow' FROM roles_v2
WHERE owner_type='organization' AND name='finance-manager';

INSERT OR IGNORE INTO subject_permission_rules_v2(
    subject_type,subject_id,user_id,permission_id,effect,valid_from
) VALUES(
    'user','user-personal','user-personal','membership.accept','allow','1970-01-01T00:00:00Z'
);

INSERT OR IGNORE INTO authorization_policies(id,version,active) VALUES
('membership-invite',1,1),
('membership-accept',1,1);

INSERT OR IGNORE INTO policy_nodes(id,policy_id,policy_version,parent_id,node_type,rule_type,config_json,position) VALUES
('membership-invite-root','membership-invite',1,NULL,'ALL',NULL,'{}',0),
('membership-invite-permission','membership-invite',1,'membership-invite-root','RULE','permission','{}',0),
('membership-accept-root','membership-accept',1,NULL,'ALL',NULL,'{}',0),
('membership-accept-permission','membership-accept',1,'membership-accept-root','RULE','permission','{}',0);

INSERT OR IGNORE INTO endpoint_bindings(
    id,method,route_template,resource_loader,intent_resolver,resource_type,action,
    policy_id,policy_version,active,scope_mode,allow_personal_fallback
) VALUES
('membership-invite','POST','/v1/organizations/:organizationID/membership-invitations','membership-invite','membership-invite','organization_membership','invite','membership-invite',1,1,'organization',0),
('membership-accept','POST','/v1/membership-invitations/accept','membership-accept','membership-accept','organization_membership','accept','membership-accept',1,1,'user',0);
