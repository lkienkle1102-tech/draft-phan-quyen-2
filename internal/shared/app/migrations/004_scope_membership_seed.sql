UPDATE endpoint_bindings SET scope_mode='organization',allow_personal_fallback=0 WHERE id='invoice-approve';

INSERT OR IGNORE INTO users(id,organization_id,active) VALUES('user-personal',NULL,1);
INSERT OR IGNORE INTO permissions(id,resource_type,action) VALUES
('membership.apply','organization_membership','apply'),
('membership.review','organization_membership','review');
INSERT OR IGNORE INTO roles(id,name) VALUES('personal-member-applicant','personal-member-applicant');
INSERT OR IGNORE INTO role_permissions(role_id,permission_id) VALUES
('personal-member-applicant','membership.apply'),
('finance','membership.review');
INSERT OR IGNORE INTO subject_user_roles(subject_type,subject_id,user_id,role_id) VALUES
('user','user-personal','user-personal','personal-member-applicant');
INSERT OR IGNORE INTO subject_features(subject_type,subject_id,feature_key,enabled) VALUES
('user','user-personal','organization_membership',1);
INSERT OR IGNORE INTO quota_counters(subject_type,subject_id,quota_key,quota_limit) VALUES
('user','user-personal','membership_applications',5);

INSERT OR IGNORE INTO authorization_policies VALUES('membership-apply',1,1),('membership-review',1,1);
INSERT OR IGNORE INTO policy_nodes VALUES
('membership-apply-root','membership-apply',1,NULL,'ALL',NULL,'{}',0),
('membership-apply-permission','membership-apply',1,'membership-apply-root','RULE','permission','{}',0),
('membership-apply-feature','membership-apply',1,'membership-apply-root','RULE','feature','{"feature":{"String":"organization_membership"}}',1),
('membership-apply-quota','membership-apply',1,'membership-apply-root','RULE','quota_available','{"quota":{"String":"membership_applications"},"cost":{"Int":1}}',2),
('membership-review-root','membership-review',1,NULL,'ALL',NULL,'{}',0),
('membership-review-permission','membership-review',1,'membership-review-root','RULE','permission','{}',0);

INSERT OR IGNORE INTO endpoint_bindings(id,method,route_template,resource_loader,intent_resolver,resource_type,action,policy_id,policy_version,active,scope_mode,allow_personal_fallback) VALUES
('membership-apply','POST','/v1/organizations/:organizationID/membership-applications','membership-apply','membership-apply','organization_membership','apply','membership-apply',1,1,'user',0),
('membership-review','POST','/v1/organizations/:organizationID/membership-applications/:applicationID/review','membership-review','membership-review','organization_membership','review','membership-review',1,1,'organization',0);
