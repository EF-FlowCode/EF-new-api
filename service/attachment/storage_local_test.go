package attachment

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"sync"
	"testing"
)

func newTestLocal(t *testing.T) *LocalStorage {
	t.Helper()
	dir := t.TempDir()
	s, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("new local: %v", err)
	}
	return s
}

func TestLocalStorage_PutGetDelete(t *testing.T) {
	s := newTestLocal(t)
	ctx := context.Background()
	key := "2026/04/hello.txt"
	body := []byte("hello world")

	if err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != string(body) {
		t.Fatalf("mismatch: %s", got)
	}
	exists, err := s.Exists(ctx, key)
	if err != nil || !exists {
		t.Fatalf("exists should be true, err=%v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// 二次删除不报错
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
	_, err = s.Get(ctx, key)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestLocalStorage_DoesNotOverwrite(t *testing.T) {
	// UUID 命名下几乎不冲突，一旦冲突必须报错而不是静默覆盖。
	s := newTestLocal(t)
	ctx := context.Background()
	key := "dup.bin"
	if err := s.Put(ctx, key, strings.NewReader("a"), 1, "application/octet-stream"); err != nil {
		t.Fatalf("first put: %v", err)
	}
	err := s.Put(ctx, key, strings.NewReader("b"), 1, "application/octet-stream")
	if err == nil {
		t.Fatalf("expected error on duplicate key")
	}
}

func TestLocalStorage_PathTraversalBlocked(t *testing.T) {
	s := newTestLocal(t)
	ctx := context.Background()
	cases := []string{
		"../outside.txt",
		"a/../../root.txt",
		"/absolute.txt",
		"..",
		"",
	}
	for _, k := range cases {
		err := s.Put(ctx, k, strings.NewReader("x"), 1, "text/plain")
		if err == nil {
			t.Errorf("put(%q) should have failed", k)
		}
		if _, err := s.Get(ctx, k); err == nil {
			t.Errorf("get(%q) should have failed", k)
		}
	}
}

func TestLocalStorage_LimitsBodyToDeclaredSize(t *testing.T) {
	// Put 声明 5 字节，但 reader 能给 100 字节；只应写 5 字节。
	s := newTestLocal(t)
	ctx := context.Background()
	key := "trim.bin"
	if err := s.Put(ctx, key, strings.NewReader("0123456789"), 5, "application/octet-stream"); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "01234" {
		t.Fatalf("expected 01234, got %q", got)
	}
}

func TestLocalStorage_SignedURLNotSupported(t *testing.T) {
	s := newTestLocal(t)
	_, err := s.SignedURL(context.Background(), "a", 0, "")
	if !errors.Is(err, ErrSignedURLNotSupported) {
		t.Fatalf("want ErrSignedURLNotSupported, got %v", err)
	}
}

func TestLocalStorage_ConcurrentWrites(t *testing.T) {
	s := newTestLocal(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := path.Join("race", "file-"+string(rune('a'+i))+".txt")
			body := []byte(key)
			if err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
				t.Errorf("concurrent put(%q) failed: %v", key, err)
				return
			}
			rc, err := s.Get(ctx, key)
			if err != nil {
				t.Errorf("concurrent get(%q) failed: %v", key, err)
				return
			}
			_, _ = io.ReadAll(rc)
			rc.Close()
		}(i)
	}
	wg.Wait()
}

func TestLocalStorage_AbsolutePath_Jail(t *testing.T) {
	s := newTestLocal(t)
	abs, err := s.AbsolutePath("a/b.txt")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if !strings.HasPrefix(abs, s.root) {
		t.Fatalf("abs outside root: %s", abs)
	}
	if _, err := s.AbsolutePath("../escape.txt"); err == nil {
		t.Fatalf("expected error for traversal")
	}
}
