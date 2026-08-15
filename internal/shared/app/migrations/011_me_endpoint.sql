INSERT OR IGNORE INTO permissions(id,resource_type,action)
VALUES('identity.read_self','identity','read_self');

INSERT OR IGNORE INTO authorization_policies(id,version,active)
VALUES('identity-read-self',1,1);

INSERT OR IGNORE INTO policy_nodes(
    id,policy_id,policy_version,parent_id,node_type,rule_type,config_json,position,purpose
) VALUES('root','identity-read-self',1,NULL,'RULE','self','{}',0,'allow');

INSERT OR IGNORE INTO endpoint_bindings(
    id,method,route_template,resource_loader,intent_resolver,resource_type,action,
    policy_id,policy_version,active,scope_mode,allow_personal_fallback
) VALUES(
    'identity-me','GET','/v1/me','me','me-read','identity','read_self',
    'identity-read-self',1,1,'user',0
);
