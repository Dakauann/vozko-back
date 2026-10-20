package conversation_repository

import (
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func WorkspaceWindowFilterIDs(db *gorm.DB, workspaceID string, entryType shared.EntryType, open bool) ([]string, error) {
	queries, ok := workspaceInboxQueries(conversation.SearchEntriesInput{
		WorkspaceID: workspaceID, EntryType: entryType, WindowOpen: &open,
	}, 50, 0)
	if !ok {
		return nil, nil
	}
	return pageEntryIDs(db, queries)
}

func ContainerWindowFilterIDs(db *gorm.DB, entryType shared.EntryType, containerID string, open bool) ([]string, error) {
	ch, ok := channelQueryFor(entryType)
	if !ok {
		return nil, nil
	}
	queries := containerInboxQueries(conversation.SearchEntriesInput{
		CampaignID: containerID, EntryType: entryType, WindowOpen: &open,
	}, ch, nil, 50, 0)
	return pageEntryIDs(db, queries)
}

func pageEntryIDs(db *gorm.DB, queries inboxQueries) ([]string, error) {
	var rows []struct {
		EntryID string `gorm:"column:entry_id"`
	}
	if err := db.Raw(queries.pageSQL, queries.pageArgs...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.EntryID
	}
	return ids, nil
}
