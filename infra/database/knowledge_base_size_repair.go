package database

import "gorm.io/gorm"

func sizeKnowledgeBaseDocuments(tx *gorm.DB) error {
	if err := tx.Exec(`
		UPDATE rag_documents
		   SET size_bytes = CASE
		         WHEN metadata->>'encoding' = 'base64' THEN (length(rtrim(content, '=')) * 3) / 4
		         ELSE octet_length(content)
		       END
		 WHERE content IS NOT NULL AND content <> ''
	`).Error; err != nil {
		return err
	}
	return tx.Exec(`
		UPDATE knowledge_bases kb
		   SET total_size_mb = COALESCE((
		         SELECT SUM(d.size_bytes)
		           FROM rag_documents d
		          WHERE d.knowledge_base_id = kb.id
		            AND d.deleted_at IS NULL
		       ), 0) / 1048576.0
	`).Error
}
