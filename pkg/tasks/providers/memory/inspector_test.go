package memoryprovider

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

func TestNewInspector(t *testing.T) {
	manager, _ := NewManager(tasks.Config{}, nil)

	inspector := NewInspector(manager)
	if inspector == nil {
		t.Fatal("Expected non-nil inspector")
	}
	if inspector.manager != manager {
		t.Error("Expected manager to be set")
	}
}

func TestInspector_InspectRuntime(t *testing.T) {
	t.Run("with manager", func(t *testing.T) {
		manager, _ := NewManager(tasks.Config{}, nil)
		inspector := NewInspector(manager)

		snapshot := inspector.InspectRuntime()
		if !snapshot.Enabled {
			t.Error("Expected Enabled=true")
		}
		if snapshot.TotalProcessed != 0 {
			t.Errorf("Expected TotalProcessed=0, got %d", snapshot.TotalProcessed)
		}
		if snapshot.TotalFailed != 0 {
			t.Errorf("Expected TotalFailed=0, got %d", snapshot.TotalFailed)
		}
	})

	t.Run("nil manager", func(t *testing.T) {
		inspector := NewInspector(nil)

		snapshot := inspector.InspectRuntime()
		if snapshot.Enabled {
			t.Error("Expected Enabled=false for nil manager")
		}
		if snapshot.Reason != "nil manager" {
			t.Errorf("Expected reason='nil manager', got %s", snapshot.Reason)
		}
	})
}

func TestInspector_OperateQueue(t *testing.T) {
	manager, _ := NewManager(tasks.Config{}, nil)
	inspector := NewInspector(manager)

	result, err := inspector.OperateQueue("default", "pause")
	if err == nil {
		t.Error("Expected error for queue operations (not supported)")
	}
	if result.Action != "" {
		t.Error("Expected empty action for unsupported operation")
	}
}

func TestInspector_OperateQueueNilManager(t *testing.T) {
	inspector := NewInspector(nil)

	result, err := inspector.OperateQueue("default", "pause")
	if err == nil {
		t.Error("Expected error for queue operations (not supported)")
	}
	if result.Action != "" {
		t.Error("Expected empty action for unsupported operation")
	}
}
