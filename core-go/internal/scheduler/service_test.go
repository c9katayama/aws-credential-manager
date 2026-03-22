package scheduler

import (
	"testing"
	"time"
)

func TestShouldRefresh(t *testing.T) {
	now := time.Now().UTC()
	if shouldRefresh(now, nil) {
		t.Fatal("nil expiration should not refresh")
	}

	later := now.Add(30 * time.Minute)
	if shouldRefresh(now, &later) {
		t.Fatal("far future expiration should not refresh")
	}

	soon := now.Add(5 * time.Minute)
	if !shouldRefresh(now, &soon) {
		t.Fatal("near expiration should refresh")
	}
}
