package tasks

import "testing"

func TestInspectRuntime_WithoutRedisURL(t *testing.T) {
	snapshot := InspectRuntime("")
	if snapshot.Enabled {
		t.Fatalf("expected disabled snapshot when redis_url is empty")
	}
	if snapshot.Reason == "" {
		t.Fatalf("expected reason for disabled snapshot")
	}
}

func TestInspectRuntime_InvalidRedisURL(t *testing.T) {
	snapshot := InspectRuntime("http://localhost:6379")
	if snapshot.Enabled {
		t.Fatalf("expected disabled snapshot for invalid redis scheme")
	}
	if snapshot.Reason == "" {
		t.Fatalf("expected reason for invalid redis scheme")
	}
}

func TestOperateQueue_Validation(t *testing.T) {
	if _, err := OperateQueue("", "critical", "pause"); err == nil {
		t.Fatalf("expected error when redis url is empty")
	}
	if _, err := OperateQueue("redis://127.0.0.1:6379/0", "", "pause"); err == nil {
		t.Fatalf("expected error when queue is empty")
	}
	if _, err := OperateQueue("redis://127.0.0.1:6379/0", "critical", "unknown"); err == nil {
		t.Fatalf("expected error for unsupported action")
	}
}
