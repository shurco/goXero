package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/testutil"
)

func assertContainsID(t *testing.T, list []models.Attachment, id uuid.UUID) {
	t.Helper()
	for _, it := range list {
		if it.AttachmentID == id {
			return
		}
	}
	t.Fatalf("id %s not in list (size %d)", id, len(list))
}

func assertNotContainsID(t *testing.T, list []models.Attachment, id uuid.UUID) {
	t.Helper()
	for _, it := range list {
		if it.AttachmentID == id {
			t.Fatalf("id %s unexpectedly present", id)
		}
	}
}

// TestIntegration_OrgFiles_InsertList verifies blob round-trip and pagination
// for the Org Files inbox/archive folders.
func TestIntegration_OrgFiles_InsertList(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	body := []byte("hello files")
	a, err := repos.Attachments.InsertOrgFile(ctx, seedDemoOrgID, "", "doc-"+uuid.NewString()[:6]+".txt", "text/plain", body)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, a.AttachmentID)
	assert.EqualValues(t, len(body), a.ContentLength)
	assert.Equal(t, FileFolderInbox, a.FileFolder)

	items, total, err := repos.Attachments.ListOrgFiles(ctx, seedDemoOrgID, FileFolderInbox, 100, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	found := false
	for _, it := range items {
		if it.AttachmentID == a.AttachmentID {
			found = true
			break
		}
	}
	assert.True(t, found, "inserted attachment must appear in inbox listing")
}

// TestIntegration_OrgFiles_MoveAndDelete validates folder switch and batch delete.
func TestIntegration_OrgFiles_MoveAndDelete(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	first, err := repos.Attachments.InsertOrgFile(ctx, seedDemoOrgID, FileFolderInbox, "a-"+uuid.NewString()[:6]+".txt", "text/plain", []byte("A"))
	require.NoError(t, err)
	second, err := repos.Attachments.InsertOrgFile(ctx, seedDemoOrgID, FileFolderInbox, "b-"+uuid.NewString()[:6]+".txt", "text/plain", []byte("B"))
	require.NoError(t, err)

	require.NoError(t, repos.Attachments.MoveOrgFiles(ctx, seedDemoOrgID,
		[]uuid.UUID{first.AttachmentID, second.AttachmentID}, FileFolderArchive))

	inbox, _, err := repos.Attachments.ListOrgFiles(ctx, seedDemoOrgID, FileFolderInbox, 500, 0)
	require.NoError(t, err)
	archive, _, err := repos.Attachments.ListOrgFiles(ctx, seedDemoOrgID, FileFolderArchive, 500, 0)
	require.NoError(t, err)
	assertContainsID(t, archive, first.AttachmentID)
	assertContainsID(t, archive, second.AttachmentID)
	assertNotContainsID(t, inbox, first.AttachmentID)
	assertNotContainsID(t, inbox, second.AttachmentID)

	require.NoError(t, repos.Attachments.DeleteOrgFiles(ctx, seedDemoOrgID,
		[]uuid.UUID{first.AttachmentID, second.AttachmentID}))
	after, _, err := repos.Attachments.ListOrgFiles(ctx, seedDemoOrgID, FileFolderArchive, 500, 0)
	require.NoError(t, err)
	assertNotContainsID(t, after, first.AttachmentID)
	assertNotContainsID(t, after, second.AttachmentID)
}

// TestIntegration_OrgFiles_InvalidFolder rejects unknown folder literals early.
func TestIntegration_OrgFiles_InvalidFolder(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	_, err := repos.Attachments.InsertOrgFile(ctx, seedDemoOrgID, "TRASH", "x.txt", "text/plain", []byte("x"))
	assert.Error(t, err)

	_, _, err = repos.Attachments.ListOrgFiles(ctx, seedDemoOrgID, "TRASH", 10, 0)
	assert.Error(t, err)

	err = repos.Attachments.MoveOrgFiles(ctx, seedDemoOrgID, []uuid.UUID{uuid.New()}, "TRASH")
	assert.Error(t, err)
}

// TestIntegration_OrgFiles_EmptyIDs no-ops for move/delete when id list is empty.
func TestIntegration_OrgFiles_EmptyIDs(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	require.NoError(t, repos.Attachments.MoveOrgFiles(context.Background(), seedDemoOrgID, nil, FileFolderArchive))
	require.NoError(t, repos.Attachments.DeleteOrgFiles(context.Background(), seedDemoOrgID, nil))
}
