package schema

import "time"

type Analysis struct {
	ID                string    `gorm:"type:uuid;primaryKey"`
	EntryID           string    `gorm:"type:uuid;not null;index:idx_analysis_entry;index:idx_analysis_entry_composite,priority:1;index:idx_analysis_entry_type_created,priority:1"`
	EntryType         string    `gorm:"type:varchar(20);not null;index:idx_analysis_entry_type;index:idx_analysis_entry_composite,priority:2;index:idx_analysis_entry_type_created,priority:2"`
	Interest          string    `gorm:"type:varchar(50);not null;index:idx_analysis_interest"`
	ProductInterest   *string   `gorm:"type:varchar(255)"`
	Disposition       string    `gorm:"type:varchar(50);not null;index:idx_analysis_disposition"`
	Sentiment         string    `gorm:"type:varchar(50);not null"`
	Qualification     string    `gorm:"type:varchar(50);not null;index:idx_analysis_qualification"`
	NextAction        string    `gorm:"type:varchar(50);not null"`
	Summary           string    `gorm:"type:text"`
	AttendanceQuality int       `gorm:"type:int;default:0;index:idx_analysis_attendance_quality"`
	MessageCount      int       `gorm:"type:int;default:0"`
	CreatedAt         time.Time `gorm:"autoCreateTime;index:idx_analysis_created_at;index:idx_analysis_entry_type_created,priority:3"`
}

func (Analysis) TableName() string {
	return "analyses"
}
