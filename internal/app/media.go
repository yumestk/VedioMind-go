package app

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yumestk/VedioMind-go/internal/platform"
)

type Media struct {
	ID string `json:"id"`
	platform.Video
	SnapshotVersion int       `json:"snapshotVersion"`
	CreatedAt       time.Time `json:"createdAt"`
}

const mediaColumns = `id::text,platform,external_id,canonical_url,title,duration_ms,source,language,snapshot_version,created_at`

type scanner interface{ Scan(...any) error }

func scanMedia(row scanner) (Media, error) {
	var m Media
	err := row.Scan(&m.ID, &m.Platform, &m.ExternalID, &m.CanonicalURL, &m.Title, &m.DurationMs, &m.Source, &m.Language, &m.SnapshotVersion, &m.CreatedAt)
	return m, err
}
func notFound(c *gin.Context) { fail(c, 404, "NOT_FOUND", "资源不存在") }
func validID(c *gin.Context, name string) bool {
	n, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || n <= 0 {
		notFound(c)
		return false
	}
	return true
}
func (s *Server) ownedMedia(c *gin.Context) (Media, User, bool) {
	u, ok := s.requireUser(c)
	if !ok {
		return Media{}, u, false
	}
	if !validID(c, "id") {
		return Media{}, u, false
	}
	m, err := scanMedia(s.DB.QueryRowContext(c.Request.Context(), `SELECT `+mediaColumns+` FROM media WHERE id=$1 AND owner_user_id=$2`, c.Param("id"), u.ID))
	if errors.Is(err, sql.ErrNoRows) {
		notFound(c)
		return m, u, false
	}
	if err != nil {
		internal(c, err)
		return m, u, false
	}
	return m, u, true
}
func (s *Server) listMedia(c *gin.Context) {
	u, ok := s.requireUser(c)
	if !ok {
		return
	}
	rows, err := s.DB.QueryContext(c.Request.Context(), `SELECT `+mediaColumns+` FROM media WHERE owner_user_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, u.ID)
	if err != nil {
		internal(c, err)
		return
	}
	defer rows.Close()
	items := []Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			internal(c, err)
			return
		}
		items = append(items, m)
	}
	if err = rows.Err(); err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, gin.H{"items": items})
}
func (s *Server) getMedia(c *gin.Context) {
	m, _, ok := s.ownedMedia(c)
	if ok {
		c.JSON(200, m)
	}
}
func (s *Server) getTranscript(c *gin.Context) {
	m, _, ok := s.ownedMedia(c)
	if !ok {
		return
	}
	segments, err := s.segments(c.Request.Context(), m.ID)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, gin.H{"snapshotVersion": m.SnapshotVersion, "segments": segments})
}
func (s *Server) segments(ctx context.Context, id string) ([]platform.Segment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,start_ms,end_ms,text FROM transcript_segments WHERE media_id=$1 ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []platform.Segment{}
	for rows.Next() {
		var item platform.Segment
		if err = rows.Scan(&item.ID, &item.StartMs, &item.EndMs, &item.Text); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Server) deleteMedia(c *gin.Context) {
	m, u, ok := s.ownedMedia(c)
	if !ok {
		return
	}
	if _, err := s.DB.ExecContext(c.Request.Context(), `DELETE FROM media WHERE id=$1 AND owner_user_id=$2`, m.ID, u.ID); err != nil {
		internal(c, err)
		return
	}
	c.Status(204)
}
func (s *Server) importMedia(c *gin.Context) {
	u, ok := s.requireUser(c)
	if !ok {
		return
	}
	var input struct {
		URL string `json:"url"`
	}
	if !decode(c, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	target, err := s.Resolver.Normalize(ctx, input.URL)
	if err != nil {
		platformFailure(c, err)
		return
	}
	existing, err := scanMedia(s.DB.QueryRowContext(ctx, `SELECT `+mediaColumns+` FROM media WHERE owner_user_id=$1 AND platform=$2 AND external_id=$3`, u.ID, target.Platform, target.ID))
	if err == nil {
		c.JSON(200, existing)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		internal(c, err)
		return
	}
	allowed, err := s.allow(ctx, "import:"+u.ID, 10, 60)
	if err != nil {
		internal(c, err)
		return
	}
	if !allowed {
		c.Header("Retry-After", "60")
		fail(c, 429, "IMPORT_RATE_LIMITED", "导入次数过多，请稍后重试")
		return
	}
	video, err := s.Resolver.Resolve(ctx, target)
	if err != nil {
		platformFailure(c, err)
		return
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		internal(c, err)
		return
	}
	defer tx.Rollback()
	m, err := scanMedia(tx.QueryRowContext(ctx, `INSERT INTO media(owner_user_id,platform,external_id,canonical_url,title,duration_ms,source,language) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(owner_user_id,platform,external_id) DO NOTHING RETURNING `+mediaColumns, u.ID, video.Platform, video.ExternalID, video.CanonicalURL, video.Title, video.DurationMs, video.Source, video.Language))
	if errors.Is(err, sql.ErrNoRows) {
		existing, err = scanMedia(tx.QueryRowContext(ctx, `SELECT `+mediaColumns+` FROM media WHERE owner_user_id=$1 AND platform=$2 AND external_id=$3`, u.ID, target.Platform, target.ID))
		if err != nil {
			internal(c, err)
			return
		}
		c.JSON(200, existing)
		return
	}
	if err != nil {
		internal(c, err)
		return
	}
	for position, segment := range video.Segments {
		if _, err = tx.ExecContext(ctx, `INSERT INTO transcript_segments(media_id,snapshot_version,id,position,start_ms,end_ms,text) VALUES($1,1,$2,$3,$4,$5,$6)`, m.ID, segment.ID, position, segment.StartMs, segment.EndMs, segment.Text); err != nil {
			internal(c, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		internal(c, err)
		return
	}
	c.JSON(201, m)
}
func platformFailure(c *gin.Context, err error) {
	var e *platform.Error
	if errors.As(err, &e) {
		if e.Status == 429 {
			c.Header("Retry-After", "60")
		}
		fail(c, e.Status, e.Code, e.Message)
		return
	}
	internal(c, err)
}
