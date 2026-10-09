package leadarea_repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/geo"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
	"vozko/infra/database"
)

const areaColumns = "id, workspace_id, owner_id, visibility, name, kind, shape, created_at, updated_at"

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) leadarea.Repository {
	return &repository{db: db}
}

type pointDoc struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type shapeDoc struct {
	Kind    string     `json:"kind"`
	Ring    []pointDoc `json:"ring,omitempty"`
	Center  *pointDoc  `json:"center,omitempty"`
	RadiusM float64    `json:"radiusM,omitempty"`
}

type areaRow struct {
	ID          string
	WorkspaceID string
	OwnerID     string
	Visibility  string
	Name        string
	Kind        string
	Shape       []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r *repository) Create(ctx context.Context, a leadarea.Area) error {
	shape, err := encodeShape(a.Shape)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(
		`INSERT INTO lead_areas (id, workspace_id, owner_id, visibility, name, kind, shape, ring, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?::polygon, ?, ?)`,
		a.ID, a.WorkspaceID, a.OwnerID, string(a.Visibility), a.Name, string(a.Shape.Kind), shape, ringText(a.Ring()), a.CreatedAt, a.UpdatedAt,
	).Error
}

func (r *repository) Update(ctx context.Context, a leadarea.Area) error {
	shape, err := encodeShape(a.Shape)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Exec(
		`UPDATE lead_areas SET visibility = ?, name = ?, kind = ?, shape = ?::jsonb, ring = ?::polygon, updated_at = ? WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL`,
		string(a.Visibility), a.Name, string(a.Shape.Kind), shape, ringText(a.Ring()), a.UpdatedAt, a.ID, a.WorkspaceID,
	)
	return touched(result)
}

func (r *repository) Delete(ctx context.Context, workspaceID, id string, at time.Time) error {
	if !isUUID(id) {
		return leadarea.ErrNotFound
	}
	result := r.db.WithContext(ctx).Exec(
		`UPDATE lead_areas SET deleted_at = ?, updated_at = ? WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL`,
		at, at, id, workspaceID,
	)
	return touched(result)
}

func (r *repository) Get(ctx context.Context, workspaceID, id string) (leadarea.Area, error) {
	if !isUUID(id) || !isUUID(workspaceID) {
		return leadarea.Area{}, leadarea.ErrNotFound
	}
	var rows []areaRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT `+areaColumns+` FROM lead_areas WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`, workspaceID, id,
	).Scan(&rows).Error
	if err != nil {
		return leadarea.Area{}, err
	}
	if len(rows) == 0 {
		return leadarea.Area{}, leadarea.ErrNotFound
	}
	return rows[0].toDomain()
}

func (r *repository) ListReadable(ctx context.Context, workspaceID, viewerID string) ([]leadarea.Area, error) {
	if !isUUID(workspaceID) || !isUUID(viewerID) {
		return []leadarea.Area{}, nil
	}
	var rows []areaRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT `+areaColumns+` FROM lead_areas WHERE workspace_id = ? AND deleted_at IS NULL AND (visibility = ? OR owner_id = ?) ORDER BY lower(name), id LIMIT ?`,
		workspaceID, string(shared.VisibilityShared), viewerID, leadarea.MaxListed,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

func (r *repository) FindLive(ctx context.Context, workspaceID string, ids []string) ([]leadarea.Area, error) {
	valid := database.UUIDArray(ids)
	if len(valid) == 0 || !isUUID(workspaceID) {
		return []leadarea.Area{}, nil
	}
	var rows []areaRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT `+areaColumns+` FROM lead_areas WHERE workspace_id = ? AND id = ANY(?::uuid[]) AND deleted_at IS NULL`, workspaceID, valid,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

func touched(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return leadarea.ErrNotFound
	}
	return nil
}

func isUUID(id string) bool {
	_, err := uuid.Parse(strings.TrimSpace(id))
	return err == nil
}

func toDomainAll(rows []areaRow) ([]leadarea.Area, error) {
	out := make([]leadarea.Area, 0, len(rows))
	for _, row := range rows {
		a, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (row areaRow) toDomain() (leadarea.Area, error) {
	shape, err := decodeShape(row.Shape)
	if err != nil {
		return leadarea.Area{}, fmt.Errorf("lead area %s: %w", row.ID, err)
	}
	return leadarea.Area{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		OwnerID:     row.OwnerID,
		Visibility:  shared.Visibility(row.Visibility),
		Name:        row.Name,
		Shape:       shape,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func encodeShape(s geo.Shape) (string, error) {
	doc := shapeDoc{Kind: string(s.Kind), RadiusM: s.RadiusM}
	for _, p := range s.Ring {
		doc.Ring = append(doc.Ring, pointDoc{Lat: p.Lat, Lng: p.Lng})
	}
	if s.Kind == geo.ShapeCircle {
		doc.Center = &pointDoc{Lat: s.Center.Lat, Lng: s.Center.Lng}
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeShape(raw []byte) (geo.Shape, error) {
	var doc shapeDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return geo.Shape{}, err
	}
	s := geo.Shape{Kind: geo.ShapeKind(doc.Kind), RadiusM: doc.RadiusM}
	for _, p := range doc.Ring {
		s.Ring = append(s.Ring, geo.Point{Lat: p.Lat, Lng: p.Lng})
	}
	if doc.Center != nil {
		s.Center = geo.Point{Lat: doc.Center.Lat, Lng: doc.Center.Lng}
	}
	return s, nil
}

func ringText(ring []geo.Point) string {
	parts := make([]string, len(ring))
	for i, p := range ring {
		parts[i] = "(" + strconv.FormatFloat(p.Lng, 'f', -1, 64) + "," + strconv.FormatFloat(p.Lat, 'f', -1, 64) + ")"
	}
	return "(" + strings.Join(parts, ",") + ")"
}

func (r *repository) CountLive(ctx context.Context, workspaceID string) (int, error) {
	if !isUUID(workspaceID) {
		return 0, leadarea.ErrWorkspaceRequired
	}
	var n int
	err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM lead_areas WHERE workspace_id = ? AND deleted_at IS NULL`, workspaceID).Scan(&n).Error
	return n, err
}
