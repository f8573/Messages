package media

import (
	"context"
	"strings"
	"testing"
)

func TestLocalStorePutAndOpen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewLocalStore(dir)
	input := "hello media"
	info, err := store.PutObject(context.Background(), "attachments/test/file.txt", strings.NewReader(input))
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}
	if info.SizeBytes != int64(len(input)) {
		t.Fatalf("unexpected size: %d", info.SizeBytes)
	}

	reader, openedInfo, err := store.OpenObject(context.Background(), "attachments/test/file.txt")
	if err != nil {
		t.Fatalf("OpenObject failed: %v", err)
	}
	defer reader.Close()
	if openedInfo.SizeBytes != info.SizeBytes {
		t.Fatalf("OpenObject size mismatch: %d vs %d", openedInfo.SizeBytes, info.SizeBytes)
	}
}
