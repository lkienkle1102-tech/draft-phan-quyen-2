INSERT INTO organizations(id,name) VALUES('org-a','Owner'),('org-b','Partner');
INSERT INTO users(id) VALUES('user-a'),('user-b'),('user-personal');
INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES
('legacy:org-a:user-a','org-a','user-a',1,'1970-01-01T00:00:00Z'),
('legacy:org-b:user-b','org-b','user-b',1,'1970-01-01T00:00:00Z');

INSERT INTO permissions(id,resource_type,action) VALUES
('invoice.approve','invoice','approve'),
('membership.apply','organization_membership','apply'),
('membership.review','organization_membership','review'),
('membership.invite','organization_membership','invite'),
('membership.accept','organization_membership','accept'),
('external_grant.manage','external_grant','manage'),
('identity.read_self','identity','read_self');

INSERT INTO features_v2(key) VALUES('invoice_management'),('organization_membership');
INSERT INTO quota_definitions_v2(key,reset_period) VALUES
('invoice_approvals','monthly'),('membership_applications','monthly');
INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES
('organization:org-a:invoice_management','organization','org-a','invoice_management','allow','manual','seed','1970-01-01T00:00:00Z'),
('organization:org-b:invoice_management','organization','org-b','invoice_management','allow','manual','seed','1970-01-01T00:00:00Z'),
('user:user-personal:organization_membership','user','user-personal','organization_membership','allow','manual','seed','1970-01-01T00:00:00Z');
INSERT INTO subject_quota_entitlements_v2(id,subject_type,subject_id,quota_key,effect,quota_limit,period_start,source_type,source_id) VALUES
('organization:org-a:invoice_approvals','organization','org-a','invoice_approvals','allow',100,'1970-01-01T00:00:00Z','manual','seed'),
('organization:org-b:invoice_approvals','organization','org-b','invoice_approvals','allow',10,'1970-01-01T00:00:00Z','manual','seed'),
('user:user-personal:membership_applications','user','user-personal','membership_applications','allow',5,'1970-01-01T00:00:00Z','manual','seed');

INSERT INTO invoices(id,organization_id,owner_id,approver_id,status,amount,region,system,version) VALUES
('invoice-a','org-a','user-a','user-a','pending',50000,'vn',0,1),
('invoice-partner','org-a','user-a','user-b','pending',25000,'vn',0,1),
('invoice-low','org-a','user-a','user-a','pending',10000,'vn',0,1);
