ALTER TABLE policy_nodes ADD COLUMN purpose TEXT NOT NULL DEFAULT 'allow'
    CHECK(purpose IN('allow','behavior'));

INSERT INTO policy_nodes(
    id,policy_id,policy_version,parent_id,node_type,rule_type,config_json,position,purpose
) VALUES(
    'invoice-low-amount','invoice-approve',1,NULL,'RULE','amount_threshold',
    '{"maximum":{"Int":30000}}',0,'behavior'
);

INSERT INTO policy_behaviors_v2(
    policy_id,policy_version,id,condition_root_id,strategy,priority,parameters_json,obligations_json
) VALUES(
    'invoice-approve',1,'invoice-manual-review','invoice-low-amount','approve',10,
    '{"reason":{"String":"low_value_manual_review"}}',
    '[{"Type":"require_manual_review","Config":{}}]'
);

INSERT INTO invoices(id,organization_id,owner_id,approver_id,status,amount,region,system,version)
VALUES('invoice-low','org-a','user-a','user-a','pending',10000,'vn',0,1);
