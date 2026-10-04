package database

import "gorm.io/gorm"

const receptiveNewestSQL = `
	WITH owned AS (
		SELECT c.*,
		       row_number() OVER (PARTITION BY c.workspace_id, c.business_phone_id ORDER BY c.created_at DESC, c.id DESC) AS rank
		  FROM whatsapp_campaigns c
		  JOIN whatsapp_business_phone_numbers p
		    ON p.id = c.business_phone_id AND p.owner_workspace_id = c.workspace_id
		 WHERE c.type = 'organic' AND NOT c.archived
	)`

const receptivePinSilentConversationsSQL = receptiveNewestSQL + `
	UPDATE whatsapp_campaign_entries AS e
	   SET automation_enabled = false
	  FROM owned AS older
	  JOIN owned AS newest
	    ON newest.workspace_id = older.workspace_id
	   AND newest.business_phone_id = older.business_phone_id
	   AND newest.rank = 1
	 WHERE older.rank > 1
	   AND e.campaign_id = older.id
	   AND e.automation_enabled IS NULL
	   AND NOT (older.enable_agent_responses OR older.enable_workflow)
	   AND (newest.enable_agent_responses OR newest.enable_workflow)`

const receptiveAlignContainersSQL = receptiveNewestSQL + `
	UPDATE whatsapp_campaigns AS c
	   SET agent_id = newest.agent_id,
	       workflow_id = newest.workflow_id,
	       pipeline_id = newest.pipeline_id,
	       enable_agent_responses = newest.enable_agent_responses,
	       enable_workflow = newest.enable_workflow,
	       enable_analysis = newest.enable_analysis,
	       enable_auto_staging = newest.enable_auto_staging,
	       enable_auto_memory = newest.enable_auto_memory,
	       updated_at = now()
	  FROM owned AS older
	  JOIN owned AS newest
	    ON newest.workspace_id = older.workspace_id
	   AND newest.business_phone_id = older.business_phone_id
	   AND newest.rank = 1
	 WHERE older.rank > 1
	   AND c.id = older.id
	   AND (c.agent_id, c.workflow_id, c.pipeline_id, c.enable_agent_responses, c.enable_workflow, c.enable_analysis, c.enable_auto_staging, c.enable_auto_memory)
	       IS DISTINCT FROM
	       (newest.agent_id, newest.workflow_id, newest.pipeline_id, newest.enable_agent_responses, newest.enable_workflow, newest.enable_analysis, newest.enable_auto_staging, newest.enable_auto_memory)`

const receptiveSilenceGrantedSQL = `
	UPDATE whatsapp_campaigns AS c
	   SET enable_agent_responses = false,
	       enable_workflow = false,
	       enable_analysis = false,
	       enable_auto_staging = false,
	       enable_auto_memory = false,
	       updated_at = now()
	  FROM whatsapp_business_phone_numbers AS p
	 WHERE p.id = c.business_phone_id
	   AND c.type = 'organic'
	   AND coalesce(p.owner_workspace_id::text, '') <> ''
	   AND p.owner_workspace_id <> c.workspace_id
	   AND (c.enable_agent_responses OR c.enable_workflow OR c.enable_analysis OR c.enable_auto_staging OR c.enable_auto_memory)`

const receptiveRenameSQL = `
	UPDATE whatsapp_campaigns
	   SET name = 'Receptivo ' || btrim(substring(name FROM '^Organic – (.*) \(coexistence\)$'))
	 WHERE type = 'organic'
	   AND name ~ '^Organic – .* \(coexistence\)$'`

func moveReceptiveToTheNumber(tx *gorm.DB) error {
	for _, statement := range []string{
		receptivePinSilentConversationsSQL,
		receptiveAlignContainersSQL,
		receptiveSilenceGrantedSQL,
		receptiveRenameSQL,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
