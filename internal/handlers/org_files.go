package handlers

import (
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/repository"
)

// maxOrgFileUploadBytes caps multipart file uploads to 25 MiB so that a single
// request cannot exhaust server memory. Larger files should use chunked upload.
const maxOrgFileUploadBytes = 25 * 1024 * 1024

// OrgFileHandler serves GET/POST /api/v1/files (Xero Files inbox).
type OrgFileHandler struct {
	repos *repository.Repositories
}

func NewOrgFileHandler(r *repository.Repositories) *OrgFileHandler {
	return &OrgFileHandler{repos: r}
}

func folderFromQuery(c fiber.Ctx) string {
	f := strings.ToUpper(strings.TrimSpace(c.Query("folder")))
	switch f {
	case repository.FileFolderArchive:
		return repository.FileFolderArchive
	default:
		return repository.FileFolderInbox
	}
}

// List GET /api/v1/files?folder=inbox|archive
func (h *OrgFileHandler) List(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	folder := folderFromQuery(c)
	p := paginationFromQuery(c)
	list, total, err := h.repos.Attachments.ListOrgFiles(c.Context(), orgID, folder, p.PageSize, p.Offset())
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{
		"Files":      list,
		"Pagination": modelsPagination(p.Page, p.PageSize, total),
	})
}

func modelsPagination(page, pageSize, total int) fiber.Map {
	return fiber.Map{
		"page":     page,
		"pageSize": pageSize,
		"total":    total,
	}
}

type orgFilesIDsBody struct {
	AttachmentIDs []string `json:"AttachmentIDs"`
}

// Move POST /api/v1/files/move  { "AttachmentIDs": [...], "Folder": "ARCHIVE"|"INBOX" }
func (h *OrgFileHandler) Move(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	var body struct {
		AttachmentIDs []string `json:"AttachmentIDs"`
		Folder        string   `json:"Folder"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return errInvalidPayload
	}
	folder := strings.ToUpper(strings.TrimSpace(body.Folder))
	if folder != repository.FileFolderInbox && folder != repository.FileFolderArchive {
		return fiber.NewError(fiber.StatusBadRequest, "Folder must be INBOX or ARCHIVE")
	}
	ids, err := parseUUIDList(body.AttachmentIDs)
	if err != nil {
		return err
	}
	if err := h.repos.Attachments.MoveOrgFiles(c.Context(), orgID, ids, folder); err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// Delete POST /api/v1/files/delete
func (h *OrgFileHandler) Delete(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	var body orgFilesIDsBody
	if err := c.Bind().Body(&body); err != nil {
		return errInvalidPayload
	}
	ids, err := parseUUIDList(body.AttachmentIDs)
	if err != nil {
		return err
	}
	if err := h.repos.Attachments.DeleteOrgFiles(c.Context(), orgID, ids); err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// Upload POST /api/v1/files  multipart: file=@..., folder=inbox|archive
func (h *OrgFileHandler) Upload(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)

	folder := repository.FileFolderInbox
	if f := strings.ToUpper(strings.TrimSpace(c.FormValue("folder"))); f == repository.FileFolderArchive {
		folder = repository.FileFolderArchive
	}

	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		return fiber.NewError(fiber.StatusBadRequest, "file is required")
	}
	if fh.Size > maxOrgFileUploadBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "file exceeds 25 MiB limit")
	}
	src, err := fh.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not read file")
	}
	defer src.Close()
	body, err := io.ReadAll(io.LimitReader(src, maxOrgFileUploadBytes+1))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not read file")
	}
	if int64(len(body)) > maxOrgFileUploadBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "file exceeds 25 MiB limit")
	}
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "empty file")
	}
	mime := fh.Header.Get("Content-Type")
	fn := fh.Filename
	if fn == "" {
		fn = "upload"
	}

	att, err := h.repos.Attachments.InsertOrgFile(c.Context(), orgID, folder, fn, mime, body)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Files", *att)
}
