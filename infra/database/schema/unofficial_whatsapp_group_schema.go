package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type UnofficialWhatsAppWorkspaceGroup struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null"`
	JID         string `gorm:"column:jid;size:64;not null"`

	Subject     string `gorm:"size:255"`
	Description string `gorm:"size:1024"`
	Origin      string `gorm:"size:16;not null;default:'external'"`
	Category    string `gorm:"size:60;not null;default:''"`
	Announce    bool   `gorm:"not null;default:false"`
	Locked      bool   `gorm:"not null;default:false"`
	MemberCount int    `gorm:"not null;default:0"`
	PictureURL  string `gorm:"size:1024"`

	CreatedByID       *string `gorm:"type:uuid"`
	PrimaryInstanceID *string `gorm:"type:uuid"`
	SeriesID          *string `gorm:"type:uuid"`
	SeriesSequence    int     `gorm:"not null;default:0"`

	FollowedAt      *time.Time `gorm:"type:timestamptz"`
	MembersSyncedAt *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt
}

func (UnofficialWhatsAppWorkspaceGroup) TableName() string {
	return "unofficial_whatsapp_workspace_groups"
}

func (g *UnofficialWhatsAppWorkspaceGroup) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	return nil
}

type UnofficialWhatsAppGroupMember struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	GroupID     string `gorm:"type:uuid;not null"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_uwgm_ws_person,priority:1"`
	PersonKey   string `gorm:"size:96;not null;index:idx_uwgm_ws_person,priority:2"`

	JID         string `gorm:"column:jid;size:64;not null"`
	LID         string `gorm:"column:lid;size:64"`
	Phone       string `gorm:"size:32"`
	DisplayName string `gorm:"size:255"`
	Role        string `gorm:"size:16;not null;default:'member'"`
	Status      string `gorm:"size:16;not null;default:'active'"`
	OurNumber   bool   `gorm:"not null;default:false"`

	JoinedAt *time.Time `gorm:"type:timestamptz"`
	LeftAt   *time.Time `gorm:"type:timestamptz"`
	LeadID   *string    `gorm:"type:uuid"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (UnofficialWhatsAppGroupMember) TableName() string {
	return "unofficial_whatsapp_group_members"
}

func (m *UnofficialWhatsAppGroupMember) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

type UnofficialWhatsAppGroupMemberEvent struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"type:uuid;not null"`
	GroupID     string    `gorm:"type:uuid;not null;index:idx_uwgme_group_at,priority:1"`
	PersonKey   string    `gorm:"size:96;not null"`
	Kind        string    `gorm:"size:8;not null"`
	OurNumber   bool      `gorm:"not null;default:false"`
	OccurredAt  time.Time `gorm:"type:timestamptz;not null;index:idx_uwgme_group_at,priority:2"`
}

func (UnofficialWhatsAppGroupMemberEvent) TableName() string {
	return "unofficial_whatsapp_group_member_events"
}

func (e *UnofficialWhatsAppGroupMemberEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}

type UnofficialWhatsAppGroupAnalysis struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	GroupID     string `gorm:"type:uuid;not null;index"`

	Name             string `gorm:"size:255;not null"`
	Prompt           string `gorm:"type:text;not null"`
	Status           string `gorm:"size:16;not null;default:'active'"`
	Trigger          string `gorm:"size:16;not null;default:'message_count'"`
	MessagesPerCycle int    `gorm:"not null;default:50"`
	ScheduleHour     int    `gorm:"not null;default:0"`
	ScheduleMinute   int    `gorm:"not null;default:0"`
	TrackedFields    []byte `gorm:"type:jsonb"`

	PendingMessages int        `gorm:"not null;default:0"`
	AnalyzedUntil   *time.Time `gorm:"type:timestamptz"`
	AnalyzedUntilID string     `gorm:"type:text;not null;default:''"`
	Failures        int        `gorm:"not null;default:0"`
	PausedReason    string     `gorm:"size:32;not null;default:''"`
	LastRunAt       *time.Time `gorm:"type:timestamptz"`
	NextRunAt       *time.Time `gorm:"type:timestamptz"`

	LeaseOwner string     `gorm:"size:64"`
	LeaseUntil *time.Time `gorm:"type:timestamptz"`

	CreatedByID *string   `gorm:"type:uuid"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt
}

func (UnofficialWhatsAppGroupAnalysis) TableName() string {
	return "unofficial_whatsapp_group_analyses"
}

func (a *UnofficialWhatsAppGroupAnalysis) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type UnofficialWhatsAppGroupAnalysisRun struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	AnalysisID  string `gorm:"type:uuid;not null;index:idx_uwgar_analysis_done,priority:1"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_uwgar_ws_done,priority:1"`
	GroupID     string `gorm:"type:uuid;not null;index:idx_uwgar_group_done,priority:1"`

	Cycle            int        `gorm:"not null"`
	Kind             string     `gorm:"size:8;not null"`
	Status           string     `gorm:"size:16;not null"`
	MessagesAnalyzed int        `gorm:"not null;default:0"`
	PeriodFrom       *time.Time `gorm:"type:timestamptz"`
	PeriodTo         *time.Time `gorm:"type:timestamptz"`

	Result     []byte         `gorm:"type:jsonb"`
	Sentiment  string         `gorm:"size:16"`
	AlertLevel string         `gorm:"size:8"`
	Positive   int            `gorm:"not null;default:0"`
	Neutral    int            `gorm:"not null;default:0"`
	Negative   int            `gorm:"not null;default:0"`
	ThemeKeys  pq.StringArray `gorm:"type:text[]"`
	Themes     pq.StringArray `gorm:"type:text[]"`

	Model            string `gorm:"size:120"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	FailureReason    string `gorm:"size:500"`

	StartedAt   time.Time `gorm:"type:timestamptz;not null"`
	CompletedAt time.Time `gorm:"type:timestamptz;not null;index:idx_uwgar_analysis_done,priority:2;index:idx_uwgar_ws_done,priority:2;index:idx_uwgar_group_done,priority:2"`
}

func (UnofficialWhatsAppGroupAnalysisRun) TableName() string {
	return "unofficial_whatsapp_group_analysis_runs"
}

func (r *UnofficialWhatsAppGroupAnalysisRun) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

type UnofficialWhatsAppGroupJoin struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	PlanID      string `gorm:"type:uuid;not null;index"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_uwgj_ws_created,priority:1"`
	InstanceID  string `gorm:"type:uuid;not null"`
	InviteLink  string `gorm:"size:255;not null"`
	GroupJID    string `gorm:"column:group_jid;size:64"`
	Sequence    int    `gorm:"not null;default:0"`
	Follow      bool   `gorm:"not null;default:false"`
	Status      string `gorm:"size:16;not null;index:idx_uwgj_due,priority:1"`
	Attempts    int    `gorm:"not null;default:0"`
	Error       string `gorm:"size:500"`

	DueAt      time.Time  `gorm:"type:timestamptz;not null;index:idx_uwgj_due,priority:2"`
	LeaseOwner string     `gorm:"size:64"`
	LeaseUntil *time.Time `gorm:"type:timestamptz"`

	CreatedByID *string   `gorm:"type:uuid"`
	CreatedAt   time.Time `gorm:"autoCreateTime;index:idx_uwgj_ws_created,priority:2"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (UnofficialWhatsAppGroupJoin) TableName() string {
	return "unofficial_whatsapp_group_joins"
}

func (j *UnofficialWhatsAppGroupJoin) BeforeCreate(tx *gorm.DB) error {
	if j.ID == "" {
		j.ID = uuid.New().String()
	}
	return nil
}
