package exitcolumns

import (
	"time"

	"github.com/google/uuid"

	"vozko/domain/buttonaction"
)

func AcknowledgementOf(by *string, at *time.Time) *buttonaction.Acknowledgement {
	if by == nil || at == nil {
		return nil
	}
	return &buttonaction.Acknowledgement{By: *by, At: at.UTC()}
}

func AcknowledgementColumns(ack *buttonaction.Acknowledgement) (*string, *time.Time) {
	if !ack.Given() {
		return nil, nil
	}
	by, at := ack.By, ack.At.UTC()
	return &by, &at
}

func ValidID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}
