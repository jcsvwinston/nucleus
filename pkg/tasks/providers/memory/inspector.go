package memoryprovider

import (
	"errors"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

type Inspector struct {
	manager *Manager
}

func NewInspector(manager *Manager) *Inspector {
	return &Inspector{manager: manager}
}

func (i *Inspector) InspectRuntime() tasks.RuntimeSnapshot {
	if i.manager == nil {
		return tasks.RuntimeSnapshot{Enabled: false, Reason: "nil manager"}
	}

	return tasks.RuntimeSnapshot{
		Enabled:        true,
		TotalProcessed: int(i.manager.processed.Load()),
		TotalFailed:    int(i.manager.failed.Load()),
		// Other stats are not tracked in simple memory provider
	}
}

func (i *Inspector) OperateQueue(queue, action string) (tasks.QueueActionResult, error) {
	return tasks.QueueActionResult{}, errors.New("memoryprovider: queue operations not supported")
}
