package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

// Org file inbox (Xero Files) — polymorphic row with subject_id = organisation_id.
const (
	SubjectOrgFile   = "ORGFILE"
	FileFolderInbox  = "INBOX"
	FileFolderArchive = "ARCHIVE"
)

// AttachmentRepository implements the polymorphic Attachments endpoint
// documented at https://developer.xero.com/documentation/api/accounting/attachments.
type AttachmentRepository struct {
	pool *pgxpool.Pool
}

type Attachment struct {
	models.Attachment
	SubjectType string
	SubjectID   uuid.UUID
}

func (r *AttachmentRepository) List(ctx context.Context, orgID uuid.UUID, subjectType string, subjectID uuid.UUID) ([]models.Attachment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT attachment_id, file_name, COALESCE(mime_type,''),
		        COALESCE(size_bytes,0), include_online
		   FROM attachments
		  WHERE organisation_id=$1 AND subject_type=$2 AND subject_id=$3
		  ORDER BY created_at DESC`,
		orgID, subjectType, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Attachment
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.AttachmentID, &a.FileName, &a.MimeType, &a.ContentLength, &a.IncludeOnline); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AttachmentRepository) Upload(
	ctx context.Context, orgID uuid.UUID,
	subjectType string, subjectID uuid.UUID,
	filename, mime string, body []byte, includeOnline bool,
) (*models.Attachment, error) {
	var a models.Attachment
	err := r.pool.QueryRow(ctx,
		`INSERT INTO attachments (
			organisation_id, subject_type, subject_id,
			file_name, mime_type, size_bytes, content, include_online)
		 VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8)
		 RETURNING attachment_id, file_name, COALESCE(mime_type,''),
		           COALESCE(size_bytes,0), include_online`,
		orgID, subjectType, subjectID,
		filename, mime, int64(len(body)), body, includeOnline,
	).Scan(&a.AttachmentID, &a.FileName, &a.MimeType, &a.ContentLength, &a.IncludeOnline)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AttachmentRepository) Fetch(ctx context.Context, orgID, attachmentID uuid.UUID) (*models.Attachment, []byte, error) {
	var a models.Attachment
	var body []byte
	err := r.pool.QueryRow(ctx,
		`SELECT attachment_id, file_name, COALESCE(mime_type,''),
		        COALESCE(size_bytes,0), include_online, content
		   FROM attachments WHERE organisation_id=$1 AND attachment_id=$2`,
		orgID, attachmentID).Scan(&a.AttachmentID, &a.FileName, &a.MimeType, &a.ContentLength, &a.IncludeOnline, &body)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	return &a, body, nil
}

// ListOrgFiles returns files for the organisation inbox or archive folder.
func (r *AttachmentRepository) ListOrgFiles(ctx context.Context, orgID uuid.UUID, folder string, limit, offset int) ([]models.Attachment, int, error) {
	folder = strings.ToUpper(strings.TrimSpace(folder))
	if folder == "" {
		folder = FileFolderInbox
	}
	if folder != FileFolderInbox && folder != FileFolderArchive {
		return nil, 0, fmt.Errorf("invalid folder")
	}
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM attachments
		 WHERE organisation_id=$1 AND subject_type=$2 AND subject_id=$1 AND COALESCE(file_folder,'')=$3`,
		orgID, SubjectOrgFile, folder,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx,
		`SELECT attachment_id, file_name, COALESCE(mime_type,''),
		        COALESCE(size_bytes,0), include_online, COALESCE(file_folder,''), created_at
		   FROM attachments
		  WHERE organisation_id=$1 AND subject_type=$2 AND subject_id=$1 AND COALESCE(file_folder,'')=$3
		  ORDER BY created_at DESC
		  LIMIT $4 OFFSET $5`,
		orgID, SubjectOrgFile, folder, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.Attachment
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.AttachmentID, &a.FileName, &a.MimeType, &a.ContentLength, &a.IncludeOnline, &a.FileFolder, &a.CreatedDateUTC); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// InsertOrgFile stores a blob in the organisation Files area.
func (r *AttachmentRepository) InsertOrgFile(
	ctx context.Context, orgID uuid.UUID,
	folder, filename, mime string, body []byte,
) (*models.Attachment, error) {
	folder = strings.ToUpper(strings.TrimSpace(folder))
	if folder == "" {
		folder = FileFolderInbox
	}
	if folder != FileFolderInbox && folder != FileFolderArchive {
		return nil, fmt.Errorf("invalid folder")
	}
	fn := strings.TrimSpace(filename)
	if fn == "" {
		return nil, fmt.Errorf("filename required")
	}
	var a models.Attachment
	err := r.pool.QueryRow(ctx,
		`INSERT INTO attachments (
			organisation_id, subject_type, subject_id,
			file_name, mime_type, size_bytes, content, include_online, file_folder)
		 VALUES ($1,$2,$1,$3,NULLIF($4,''),$5,$6,false,$7)
		 RETURNING attachment_id, file_name, COALESCE(mime_type,''),
		           COALESCE(size_bytes,0), include_online, COALESCE(file_folder,''), created_at`,
		orgID, SubjectOrgFile, fn, mime, int64(len(body)), body, folder,
	).Scan(&a.AttachmentID, &a.FileName, &a.MimeType, &a.ContentLength, &a.IncludeOnline, &a.FileFolder, &a.CreatedDateUTC)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// MoveOrgFiles sets folder for the given attachment ids (ORGFILE rows only).
func (r *AttachmentRepository) MoveOrgFiles(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, folder string) error {
	if len(ids) == 0 {
		return nil
	}
	folder = strings.ToUpper(strings.TrimSpace(folder))
	if folder != FileFolderInbox && folder != FileFolderArchive {
		return fmt.Errorf("invalid folder")
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE attachments SET file_folder=$3
		  WHERE organisation_id=$1 AND subject_type=$4 AND subject_id=$1
		    AND attachment_id = ANY($2::uuid[])`,
		orgID, ids, folder, SubjectOrgFile,
	)
	return err
}

// DeleteOrgFiles removes organisation file rows.
func (r *AttachmentRepository) DeleteOrgFiles(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`DELETE FROM attachments
		  WHERE organisation_id=$1 AND subject_type=$2 AND subject_id=$1
		    AND attachment_id = ANY($3::uuid[])`,
		orgID, SubjectOrgFile, ids,
	)
	return err
}

// HistoryRepository mirrors the History & Notes endpoint.
type HistoryRepository struct {
	pool *pgxpool.Pool
}

func (r *HistoryRepository) List(ctx context.Context, orgID uuid.UUID, subjectType string, subjectID uuid.UUID) ([]models.HistoryRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT h.history_id, h.changes, h.details, h.date_utc, COALESCE(u.email,'')
		   FROM history_records h
		   LEFT JOIN users u ON u.user_id = h.user_id
		  WHERE h.organisation_id=$1 AND h.subject_type=$2 AND h.subject_id=$3
		  ORDER BY h.date_utc DESC`,
		orgID, subjectType, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.HistoryRecord
	for rows.Next() {
		var h models.HistoryRecord
		if err := rows.Scan(&h.HistoryID, &h.Changes, &h.Details, &h.DateUTC, &h.User); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Add writes a raw (Changes, Details) history record — used by the invoice
// email flow and other server-side events that aren't plain user notes.
func (r *HistoryRepository) Add(
	ctx context.Context, orgID uuid.UUID,
	subjectType string, subjectID uuid.UUID,
	rec models.HistoryRecord,
) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO history_records (organisation_id, subject_type, subject_id, changes, details)
		 VALUES ($1,$2,$3,$4,$5)`,
		orgID, subjectType, subjectID, rec.Changes, rec.Details)
	return err
}

func (r *HistoryRepository) AddNote(
	ctx context.Context, orgID uuid.UUID,
	subjectType string, subjectID uuid.UUID,
	userID *uuid.UUID, details string,
) (*models.HistoryRecord, error) {
	var h models.HistoryRecord
	err := r.pool.QueryRow(ctx,
		`INSERT INTO history_records (organisation_id, subject_type, subject_id, changes, details, user_id)
		 VALUES ($1,$2,$3,'Note',$4,$5)
		 RETURNING history_id, changes, details, date_utc`,
		orgID, subjectType, subjectID, details, userID,
	).Scan(&h.HistoryID, &h.Changes, &h.Details, &h.DateUTC)
	if err != nil {
		return nil, err
	}
	return &h, nil
}
