package conversation_repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
	"vozko/infra/database/schema"
)

func senderTestMessage(sentBy conversation.SentBy) *conversation.Message {
	external := "3EB0ECHO"
	return &conversation.Message{
		ID: "msg-1", EntryID: "entry-1", EntryType: "unofficial_whatsapp",
		MessageType: conversation.MessageTypeMedia, From: "instance-1", Text: "catalogo.pdf",
		ExternalMessageID: &external, SentBy: sentBy,
	}
}

func TestCreateRefusesAMessageWithoutASender(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	err := NewRepository(db).Create(senderTestMessage(conversation.SentBy{}))

	if !errors.Is(err, conversation.ErrMessageSenderRequired) {
		t.Fatalf("Create() = %v, want %v", err, conversation.ErrMessageSenderRequired)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheStoredRowCarriesTheSenderAndItsDirection(t *testing.T) {
	row := mapDomainToSchema(senderTestMessage(conversation.SentByWorkflow("wf-1")))

	if row.SenderKind != "workflow" || row.SenderID != "workflow:wf-1" {
		t.Fatalf("stored sender %q %q, want workflow workflow:wf-1", row.SenderKind, row.SenderID)
	}
	if row.Direction != string(conversation.MessageDirectionOutbound) {
		t.Fatalf("direction %q: workflow media is outbound", row.Direction)
	}
}

func TestTheSenderIsReadBack(t *testing.T) {
	msg := mapSchemaToDomain(&schema.ConversationMessage{ID: "msg-1", SenderKind: "campaign", SenderID: "campaign:camp-1"})

	if msg.SentBy != conversation.SentByCampaign("camp-1") {
		t.Fatalf("read back %+v", msg.SentBy)
	}
}

func TestOurSendClaimsTheEchoThatArrivedFirst(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "conversation_messages"`).
		WillReturnError(errors.New(`ERROR: duplicate key value violates unique constraint "ux_cm_entry_external_msgid" (SQLSTATE 23505)`))
	mock.ExpectExec(`UPDATE "conversation_messages" SET "sender_id"=\$1,"sender_kind"=\$2,"updated_at"=\$3 WHERE .*entry_type = \$4 AND entry_id = \$5 AND external_message_id = \$6 AND sender_kind = \$7`).
		WithArgs("workflow:wf-1", "workflow", sqlmock.AnyArg(), "unofficial_whatsapp", "entry-1", "3EB0ECHO", "external").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).Create(senderTestMessage(conversation.SentByWorkflow("wf-1"))); err != nil {
		t.Fatalf("Create() = %v, want the echo claimed", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestADuplicateThatIsNotAnEchoStaysAnError(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	duplicate := errors.New(`ERROR: duplicate key value violates unique constraint "ux_cm_entry_external_msgid" (SQLSTATE 23505)`)
	mock.ExpectExec(`INSERT INTO "conversation_messages"`).WillReturnError(duplicate)
	mock.ExpectExec(`UPDATE "conversation_messages" SET`).WillReturnResult(sqlmock.NewResult(0, 0))

	err := NewRepository(db).Create(senderTestMessage(conversation.SentByWorkflow("wf-1")))

	if !errors.Is(err, duplicate) {
		t.Fatalf("Create() = %v, want the duplicate error", err)
	}
}

func TestAnEchoNeverTakesOverAnAttributedMessage(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	duplicate := errors.New(`ERROR: duplicate key value violates unique constraint "ux_cm_entry_external_msgid" (SQLSTATE 23505)`)
	mock.ExpectExec(`INSERT INTO "conversation_messages"`).WillReturnError(duplicate)

	err := NewRepository(db).Create(senderTestMessage(conversation.SentExternally()))

	if !errors.Is(err, duplicate) {
		t.Fatalf("Create() = %v, want the duplicate error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
